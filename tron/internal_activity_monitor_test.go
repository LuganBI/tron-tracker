package tron

import (
	"encoding/hex"
	"strings"
	"testing"

	"tron-tracker/common"
	"tron-tracker/config"
	"tron-tracker/tron/types"
)

func TestNewActivityMonitorEnablesInternalCombinationWithoutThresholds(t *testing.T) {
	t.Setenv("AIOPS_APP_KEYS", "app-one, app-two, app-one, ,")
	monitor := NewActivityMonitor(&config.OnChainMonitorConfig{Enabled: true})
	if monitor == nil {
		t.Fatal("monitor = nil, want suicide + Stake 2.0 monitoring with zero amount thresholds")
	}
	if len(monitor.detectors) != 0 {
		t.Fatalf("detector count = %d, want 0", len(monitor.detectors))
	}
	if got := strings.Join(monitor.aiopsAppKeys, ","); got != "app-one,app-two" {
		t.Fatalf("AIOps app keys = %q, want app-one,app-two", got)
	}
}

func TestDetectSuicideWithStake2(t *testing.T) {
	contract1 := "41" + strings.Repeat("11", 20)
	contract2 := "41" + strings.Repeat("22", 20)
	internalTxs := []types.InternalTx{
		{Note: encodeInternalNote("suicide"), From: contract1},
		{Note: encodeInternalNote("freezeBalanceV2ForBandwidth"), From: contract1},
		{Note: encodeInternalNote("delegateResourceOfEnergy"), From: contract2},
		// Rejected internals must not affect either count.
		{Note: encodeInternalNote("suicide"), From: contract2, Rejected: true},
		{Note: encodeInternalNote("unfreezeBalanceV2ForEnergy"), From: contract2, Rejected: true},
	}

	activity, ok := detectSuicideWithStake2(internalTxs, 123, 7, "tx-hash")
	if !ok {
		t.Fatal("detectSuicideWithStake2() = false, want true")
	}
	if activity.SuicideCount != 1 || activity.Stake2Count != 2 {
		t.Fatalf("counts = suicide:%d stake2:%d, want 1/2", activity.SuicideCount, activity.Stake2Count)
	}
	if got, want := strings.Join(activity.Stake2Actions, ","),
		"freezeBalanceV2ForBandwidth,delegateResourceOfEnergy"; got != want {
		t.Fatalf("actions = %q, want %q", got, want)
	}
	if got, want := strings.Join(activity.Contracts, ","),
		common.EncodeToBase58(contract1)+","+common.EncodeToBase58(contract2); got != want {
		t.Fatalf("contracts = %q, want %q", got, want)
	}

	message := slackMessageText(formatSuicideStake2Alert(activity))
	for _, want := range []string{
		"TRON High-risk Transaction Alert",
		"*Risk Level*\n`HIGH`",
		"*Block / Index*\n`123 / 7`",
		"https://tronscan.io/#/transaction/tx-hash",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("alert message missing %q: %s", want, message)
		}
	}
	for _, sensitive := range []string{
		"suicide",
		"Stake 2.0",
		"freezeBalanceV2ForBandwidth",
		"delegateResourceOfEnergy",
		common.EncodeToBase58(contract1),
		common.EncodeToBase58(contract2),
	} {
		if strings.Contains(message, sensitive) {
			t.Fatalf("alert message contains sensitive detail %q: %s", sensitive, message)
		}
	}

	aiopsAlert := formatSuicideStake2AIOpsAlert(activity)
	if aiopsAlert.EventID != "tx-hash" || aiopsAlert.EventType != "trigger" || aiopsAlert.Priority != 5 {
		t.Fatalf("AIOps alert = %#v, want fatal trigger for tx-hash", aiopsAlert)
	}
	for _, want := range []string{
		"高风险链上交易",
		"123/7",
		"tx-hash",
	} {
		if !strings.Contains(aiopsAlert.AlarmContent, want) {
			t.Fatalf("AIOps content missing %q: %s", want, aiopsAlert.AlarmContent)
		}
	}
	for _, sensitive := range []string{
		"suicide",
		"Stake 2.0",
		"freezeBalanceV2ForBandwidth",
		"delegateResourceOfEnergy",
		common.EncodeToBase58(contract1),
		common.EncodeToBase58(contract2),
	} {
		if strings.Contains(aiopsAlert.AlarmName+aiopsAlert.AlarmContent, sensitive) {
			t.Fatalf("AIOps alert contains sensitive detail %q: %#v", sensitive, aiopsAlert)
		}
	}
	if len(aiopsAlert.Contexts) != 1 || !strings.HasSuffix(aiopsAlert.Contexts[0].Href, "/tx-hash") {
		t.Fatalf("AIOps contexts = %#v, want Tronscan tx link", aiopsAlert.Contexts)
	}
}

func TestDetectSuicideWithStake2RequiresBothSuccessfulKindsInSameTransaction(t *testing.T) {
	tests := []struct {
		name        string
		internalTxs []types.InternalTx
	}{
		{
			name:        "suicide only",
			internalTxs: []types.InternalTx{{Note: encodeInternalNote("suicide")}},
		},
		{
			name:        "stake2 only",
			internalTxs: []types.InternalTx{{Note: encodeInternalNote("unfreezeBalanceV2ForEnergy")}},
		},
		{
			name: "rejected suicide",
			internalTxs: []types.InternalTx{
				{Note: encodeInternalNote("suicide"), Rejected: true},
				{Note: encodeInternalNote("cancelAllUnfreezeV2")},
			},
		},
		{
			name: "rejected stake2",
			internalTxs: []types.InternalTx{
				{Note: encodeInternalNote("suicide")},
				{Note: encodeInternalNote("withdrawExpireUnfreeze"), Rejected: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if activity, ok := detectSuicideWithStake2(tt.internalTxs, 1, 0, "tx"); ok {
				t.Fatalf("detectSuicideWithStake2() = true, activity = %#v", activity)
			}
		})
	}
}

func TestIsStake2InternalNote(t *testing.T) {
	for _, note := range []string{
		"freezeBalanceV2ForBandwidth",
		"unfreezeBalanceV2ForEnergy",
		"withdrawExpireUnfreeze",
		"withdrawExpireUnfreezeWhileSuiciding",
		"cancelAllUnfreezeV2",
		"delegateResourceOfBandwidth",
		"unDelegateResourceOfEnergy",
	} {
		if !isStake2InternalNote(note) {
			t.Errorf("isStake2InternalNote(%q) = false, want true", note)
		}
	}

	for _, note := range []string{"suicide", "call", "freezeForEnergy", "unfreezeForBandwidth"} {
		if isStake2InternalNote(note) {
			t.Errorf("isStake2InternalNote(%q) = true, want false", note)
		}
	}
}

func TestDecodeInternalNoteAcceptsHexAndPlainText(t *testing.T) {
	if got := decodeInternalNote(encodeInternalNote("suicide")); got != "suicide" {
		t.Fatalf("decoded hex note = %q, want suicide", got)
	}
	if got := decodeInternalNote("suicide"); got != "suicide" {
		t.Fatalf("decoded plain note = %q, want suicide", got)
	}
}

func TestTruncateRunesPreservesUTF8(t *testing.T) {
	if got := truncateRunes("中文告警内容", 5); got != "中文告警…" {
		t.Fatalf("truncateRunes() = %q, want 中文告警…", got)
	}
}

func encodeInternalNote(note string) string {
	return hex.EncodeToString([]byte(note))
}
