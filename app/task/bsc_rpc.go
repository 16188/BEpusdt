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
	"strings"
	"time"

	"github.com/v03413/bepusdt/app/conf"
	"github.com/v03413/bepusdt/app/log"
	"github.com/v03413/bepusdt/app/model"
)

const (
	bscChainlistURL     = "https://chainlist.org/rpcs.json"
	bscSuccessThreshold = 95.0
	bscProbeAttempts    = 3
)

type chainlistChain struct {
	ChainID int `json:"chainId"`
	RPC     []struct {
		URL      string `json:"url"`
		Tracking string `json:"tracking"`
	} `json:"rpc"`
}

type rpcResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
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

	for _, endpoint := range candidates {
		if strings.TrimRight(endpoint, "/") == strings.TrimRight(current, "/") {
			continue
		}
		if err := probeBscRPC(ctx, e.Client, endpoint); err != nil {
			continue
		}

		model.SetK(model.RpcEndpointBsc, endpoint)
		if model.GetC(model.RpcEndpointBsc) != endpoint {
			log.Task.Warn("BSC RPC 自动切换：保存新节点失败")
			return
		}

		conf.ResetStats(conf.Bsc)
		log.Task.Warn(fmt.Sprintf("BSC RPC 成功率低于 %.0f%%，已自动切换：%s → %s", bscSuccessThreshold, current, endpoint))
		return
	}

	log.Task.Warn("BSC RPC 自动切换：Chainlist 当前没有通过连续健康检查的节点")
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
			if rpc.Tracking == "yes" || rpc.Tracking == "limited" || !safePublicRPC(rpc.URL) || seen[rpc.URL] {
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

func probeBscRPC(ctx context.Context, client *http.Client, endpoint string) error {
	payload := []byte(`[
		{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1},
		{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":2},
		{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["latest",false],"id":3},
		{"jsonrpc":"2.0","method":"eth_getLogs","params":[{"fromBlock":"latest","toBlock":"latest","topics":["0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"]}],"id":4}
	]`)

	for attempt := 0; attempt < bscProbeAttempts; attempt++ {
		requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			cancel()
			return err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			cancel()
			return err
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		resp.Body.Close()
		cancel()
		if readErr != nil {
			return readErr
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("HTTP %d", resp.StatusCode)
		}

		var replies []rpcResponse
		if err := json.Unmarshal(body, &replies); err != nil {
			return err
		}
		if err := validateBscReplies(replies); err != nil {
			return err
		}
	}

	return nil
}

func validateBscReplies(replies []rpcResponse) error {
	if len(replies) != 4 {
		return fmt.Errorf("RPC 批量响应数量错误")
	}
	seen := make(map[int]bool, len(replies))
	for _, reply := range replies {
		if len(reply.Error) > 0 && string(reply.Error) != "null" {
			return fmt.Errorf("RPC %d 返回错误", reply.ID)
		}
		if len(reply.Result) == 0 || string(reply.Result) == "null" {
			return fmt.Errorf("RPC %d 缺少结果", reply.ID)
		}
		seen[reply.ID] = true
		if reply.ID == 1 {
			var chainID string
			if err := json.Unmarshal(reply.Result, &chainID); err != nil || !strings.EqualFold(chainID, "0x38") {
				return fmt.Errorf("RPC 链 ID 不是 BSC 主网")
			}
		}
	}
	for id := 1; id <= 4; id++ {
		if !seen[id] {
			return fmt.Errorf("RPC 缺少响应 %d", id)
		}
	}
	return nil
}
