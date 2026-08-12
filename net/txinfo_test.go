package net

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"tron-tracker/config"
)

func TestGetTransactionInfoListRequestsHexVisibilityAndKeepsInternals(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wallet/gettransactioninfobyblocknum" {
			t.Errorf("path = %q, want /wallet/gettransactioninfobyblocknum", r.URL.Path)
		}
		if got := r.URL.Query().Get("num"); got != "123" {
			t.Errorf("num = %q, want 123", got)
		}
		if values, exists := r.URL.Query()["visible"]; !exists || len(values) != 1 || values[0] != "false" {
			t.Errorf("visible query = %#v, want explicit false", values)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"tx-hash","internal_transactions":[{` +
			`"caller_address":"411111111111111111111111111111111111111111",` +
			`"transferTo_address":"412222222222222222222222222222222222222222",` +
			`"note":"73756963696465","rejected":false}]}]`))
	}))
	defer server.Close()

	previousConfigs := configs
	configs = &config.NetConfig{FullNode: server.URL}
	t.Cleanup(func() { configs = previousConfigs })

	infos, err := GetTransactionInfoList(123)
	if err != nil {
		t.Fatalf("GetTransactionInfoList() error = %v", err)
	}
	if len(infos) != 1 || len(infos[0].InternalTxs) != 1 {
		t.Fatalf("transaction infos = %#v, want one internal transaction", infos)
	}
	internal := infos[0].InternalTxs[0]
	if internal.Note != "73756963696465" {
		t.Fatalf("internal note = %q, want hex note", internal.Note)
	}
	if internal.From != "411111111111111111111111111111111111111111" {
		t.Fatalf("internal caller = %q, want hex address", internal.From)
	}
}
