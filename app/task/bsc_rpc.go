package task

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/v03413/bepusdt/app/conf"
	"github.com/v03413/bepusdt/app/log"
	"github.com/v03413/bepusdt/app/model"
)

const (
	bscChainlistURL     = "https://chainlist.org/rpcs.json"
	bscSuccessThreshold = 95.0
	bscTestAttempts     = 20
	bscRankWorkers      = 10
)

type chainlistChain struct {
	ChainID int `json:"chainId"`
	RPC     []struct {
		URL string `json:"url"`
	} `json:"rpc"`
}

type rpcResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

type bscRPCScore struct {
	Endpoint    string
	Height      uint64
	SuccessRate float64
}

func (e *evm) failoverBscRPC(ctx context.Context) {
	if conf.SuccessRate(conf.Bsc) >= bscSuccessThreshold {
		return
	}

	current := e.rpcEndpoint()
	candidates, err := loadBscRPCs(ctx, e.Client, bscChainlistURL)
	if err != nil {
		log.Task.Warn("BSC RPC 自动切换：获取 Chainlist 节点失败:", err)
		return
	}

	ranked := rankBscRPCs(ctx, e.Client, candidates)
	if len(ranked) == 0 {
		log.Task.Warn("BSC RPC 自动切换：Chainlist 当前没有通过基础检查的节点")
		return
	}

	var best, selected bscRPCScore
	for _, candidate := range ranked {
		candidate.SuccessRate, candidate.Height = measureBscRPC(ctx, e.Client, candidate.Endpoint, bscTestAttempts)
		if ctx.Err() != nil {
			return
		}
		log.Task.Info(fmt.Sprintf("BSC RPC 自动测试：%s 高度 %d 成功率 %.2f%%", candidate.Endpoint, candidate.Height, candidate.SuccessRate))
		if best.Endpoint == "" || candidate.SuccessRate > best.SuccessRate {
			best = candidate
		}
		if candidate.SuccessRate >= bscSuccessThreshold {
			selected = candidate
			break
		}
	}
	if selected.Endpoint == "" {
		selected = best
		log.Task.Warn(fmt.Sprintf("BSC RPC 自动切换：没有节点达到 %.0f%%，采用实测最高节点 %s（%.2f%%）", bscSuccessThreshold, selected.Endpoint, selected.SuccessRate))
	}

	if err := model.SetK(model.RpcEndpointBsc, selected.Endpoint); err != nil {
		log.Task.Warn("BSC RPC 自动切换：保存新节点失败:", err)
		return
	}

	conf.ResetStats(conf.Bsc)
	log.Task.Warn(fmt.Sprintf("BSC RPC 成功率低于 %.0f%%，已采用测试成功率 %.2f%% 的节点：%s → %s", bscSuccessThreshold, selected.SuccessRate, current, selected.Endpoint))
}

func loadBscRPCs(ctx context.Context, client *http.Client, source string) ([]string, error) {
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, source, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Chainlist HTTP %d", resp.StatusCode)
	}

	var chains []chainlistChain
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&chains); err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	for _, chain := range chains {
		if chain.ChainID != 56 {
			continue
		}

		result := make([]string, 0, len(chain.RPC))
		for _, rpc := range chain.RPC {
			if !safePublicRPC(rpc.URL) || seen[rpc.URL] {
				continue
			}
			seen[rpc.URL] = true
			result = append(result, rpc.URL)
		}
		if len(result) == 0 {
			return nil, fmt.Errorf("Chainlist 没有可用的 BSC HTTPS RPC")
		}
		return result, nil
	}

	return nil, fmt.Errorf("Chainlist 没有 chainId 56")
}

func safePublicRPC(raw string) bool {
	if strings.Contains(raw, "${") {
		return false
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return false
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		return ip.IsGlobalUnicast() && !ip.IsPrivate()
	}
	return true
}

func rankBscRPCs(ctx context.Context, client *http.Client, endpoints []string) []bscRPCScore {
	results := make(chan bscRPCScore, len(endpoints))
	workers := make(chan struct{}, bscRankWorkers)
	var wg sync.WaitGroup
	for _, endpoint := range endpoints {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case workers <- struct{}{}:
				defer func() { <-workers }()
			case <-ctx.Done():
				return
			}

			height, err := callBscRPC(ctx, client, endpoint)
			if err == nil {
				results <- bscRPCScore{Endpoint: endpoint, Height: height}
			}
		}()
	}
	wg.Wait()
	close(results)

	ranked := make([]bscRPCScore, 0, len(results))
	for result := range results {
		ranked = append(ranked, result)
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Height == ranked[j].Height {
			return ranked[i].Endpoint < ranked[j].Endpoint
		}
		return ranked[i].Height > ranked[j].Height
	})
	return ranked
}

func measureBscRPC(ctx context.Context, client *http.Client, endpoint string, attempts int) (float64, uint64) {
	var successes int
	var latestHeight uint64
	for range attempts {
		height, err := callBscRPC(ctx, client, endpoint)
		if err == nil {
			successes++
			if height > latestHeight {
				latestHeight = height
			}
		}
		if ctx.Err() != nil {
			break
		}
	}
	return float64(successes) / float64(attempts) * 100, latestHeight
}

func callBscRPC(ctx context.Context, client *http.Client, endpoint string) (uint64, error) {
	payload := []byte(`[
		{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1},
		{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":2},
		{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["latest",false],"id":3},
		{"jsonrpc":"2.0","method":"eth_getLogs","params":[{"fromBlock":"latest","toBlock":"latest","topics":["0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"]}],"id":4}
	]`)

	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	resp.Body.Close()
	if readErr != nil {
		return 0, readErr
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var replies []rpcResponse
	if err := json.Unmarshal(body, &replies); err != nil {
		return 0, err
	}
	return validateBscReplies(replies)
}

func validateBscReplies(replies []rpcResponse) (uint64, error) {
	if len(replies) != 4 {
		return 0, fmt.Errorf("RPC 批量响应数量错误")
	}
	seen := make(map[int]bool, len(replies))
	var height uint64
	for _, reply := range replies {
		if len(reply.Error) > 0 && string(reply.Error) != "null" {
			return 0, fmt.Errorf("RPC %d 返回错误", reply.ID)
		}
		if len(reply.Result) == 0 || string(reply.Result) == "null" {
			return 0, fmt.Errorf("RPC %d 缺少结果", reply.ID)
		}
		seen[reply.ID] = true
		switch reply.ID {
		case 1:
			var chainID string
			if err := json.Unmarshal(reply.Result, &chainID); err != nil || !strings.EqualFold(chainID, "0x38") {
				return 0, fmt.Errorf("RPC 链 ID 不是 BSC 主网")
			}
		case 2:
			var blockNumber string
			if err := json.Unmarshal(reply.Result, &blockNumber); err != nil {
				return 0, fmt.Errorf("RPC 区块高度格式错误")
			}
			var err error
			height, err = strconv.ParseUint(strings.TrimPrefix(strings.ToLower(blockNumber), "0x"), 16, 64)
			if err != nil || height == 0 {
				return 0, fmt.Errorf("RPC 区块高度无效")
			}
		}
	}
	for id := 1; id <= 4; id++ {
		if !seen[id] {
			return 0, fmt.Errorf("RPC 缺少响应 %d", id)
		}
	}
	return height, nil
}
