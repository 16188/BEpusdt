package task

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/v03413/bepusdt/app/conf"
)

func TestBscRPCFailoverHelpers(t *testing.T) {
	var measuredCalls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `[{"chainId":56,"rpc":[{"url":"http://unsafe.example"},{"url":"https://rpc.example","tracking":"none"}]}]`)
			return
		}
		chainID := "0x38"
		if r.URL.Path == "/measured" && measuredCalls.Add(1) == bscTestAttempts {
			chainID = "0x1"
		}
		height := "0x123"
		if r.URL.Path == "/low" {
			height = "0x122"
		}
		fmt.Fprintf(w, `[
			{"jsonrpc":"2.0","id":1,"result":"%s"},
			{"jsonrpc":"2.0","id":2,"result":"%s"},
			{"jsonrpc":"2.0","id":3,"result":{"number":"0x123"}},
			{"jsonrpc":"2.0","id":4,"result":[]}
		]`, chainID, height)
	}))
	defer server.Close()

	endpoints, err := loadBscRPCs(context.Background(), server.Client(), server.URL)
	if err != nil || len(endpoints) != 1 || endpoints[0] != "https://rpc.example" {
		t.Fatalf("unexpected Chainlist endpoints: %v, %v", endpoints, err)
	}
	rate, height := measureBscRPC(context.Background(), server.Client(), server.URL+"/measured", bscTestAttempts)
	if rate != 95 || height != 0x123 {
		t.Fatalf("expected 95%% at height 0x123, got %.2f%% at %#x", rate, height)
	}
	ranked := rankBscRPCs(context.Background(), server.Client(), []string{server.URL + "/low", server.URL + "/high"})
	if len(ranked) != 2 || ranked[0].Endpoint != server.URL+"/high" {
		t.Fatalf("expected height-descending RPC order, got %#v", ranked)
	}

	network := "bsc-failover-test"
	conf.ResetStats(network)
	conf.RecordFailure(network)
	for range 18 {
		conf.RecordSuccess(network, "1")
	}
	if conf.SuccessRate(network) >= bscSuccessThreshold {
		t.Fatalf("expected rate below threshold, got %s", conf.GetSuccessRate(network))
	}
	conf.ResetStats(network)
	if conf.SuccessRate(network) != 100 {
		t.Fatalf("expected reset rate to be 100, got %s", conf.GetSuccessRate(network))
	}
}
