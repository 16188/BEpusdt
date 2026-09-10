package task

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/v03413/bepusdt/app/conf"
)

func TestBscRPCFailoverHelpers(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			fmt.Fprintf(w, `[{"chainId":56,"rpc":[{"url":"http://unsafe.example"},{"url":"%s","tracking":"none"}]}]`, serverURL(r))
			return
		}
		fmt.Fprint(w, `[
			{"jsonrpc":"2.0","id":1,"result":"0x38"},
			{"jsonrpc":"2.0","id":2,"result":"0x123"},
			{"jsonrpc":"2.0","id":3,"result":{"number":"0x123"}},
			{"jsonrpc":"2.0","id":4,"result":[]}
		]`)
	}))
	defer server.Close()

	endpoints, err := loadBscRPCs(context.Background(), server.Client(), server.URL)
	if err != nil || len(endpoints) != 1 || endpoints[0] != server.URL {
		t.Fatalf("unexpected Chainlist endpoints: %v, %v", endpoints, err)
	}
	if err := probeBscRPC(context.Background(), server.Client(), endpoints[0]); err != nil {
		t.Fatal(err)
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

func serverURL(r *http.Request) string {
	return "https://" + r.Host
}
