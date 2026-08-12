package tron

import (
	"encoding/hex"
	"fmt"
	"strings"

	"tron-tracker/common"
	"tron-tracker/net"
	"tron-tracker/tron/types"

	"go.uber.org/zap"
)

const suicideInternalNote = "suicide"

type suicideStake2InternalActivity struct {
	Height        uint
	Index         uint16
	TxID          string
	SuicideCount  int
	Stake2Count   int
	Stake2Actions []string
	Contracts     []string
}

// ReportSuicideWithStake2 reports a transaction only when it contains both a
// successful SUICIDE internal transaction and a successful Stake 2.0-related
// internal transaction. The combination is suspicious regardless of amount,
// so it deliberately does not use the large-activity thresholds.
func (m *ActivityMonitor) ReportSuicideWithStake2(
	internalTxs []types.InternalTx, height uint, index uint16, txID string,
) {
	activity, ok := detectSuicideWithStake2(internalTxs, height, index, txID)
	if m == nil || !ok {
		return
	}

	net.ReportOnChainMonitorAndWarningMessageToSlack(m.webhook, formatSuicideStake2Alert(activity))
	if err := net.ReportAIOpsAlert(m.aiopsAppKeys, formatSuicideStake2AIOpsAlert(activity)); err != nil {
		zap.S().Errorf("report high-risk transaction alert to AIOps failed: %v", err)
	}
}

func detectSuicideWithStake2(
	internalTxs []types.InternalTx, height uint, index uint16, txID string,
) (suicideStake2InternalActivity, bool) {
	activity := suicideStake2InternalActivity{
		Height: height,
		Index:  index,
		TxID:   txID,
	}
	actionSeen := make(map[string]struct{})
	contractSeen := make(map[string]struct{})

	for _, internalTx := range internalTxs {
		if internalTx.Rejected {
			continue
		}

		note := decodeInternalNote(internalTx.Note)
		switch {
		case note == suicideInternalNote:
			activity.SuicideCount++
			appendInternalAddress(&activity.Contracts, contractSeen, internalTx.From)
		case isStake2InternalNote(note):
			activity.Stake2Count++
			if _, exists := actionSeen[note]; !exists {
				actionSeen[note] = struct{}{}
				activity.Stake2Actions = append(activity.Stake2Actions, note)
			}
			appendInternalAddress(&activity.Contracts, contractSeen, internalTx.From)
		}
	}

	return activity, activity.SuicideCount > 0 && activity.Stake2Count > 0
}

func decodeInternalNote(note string) string {
	raw := strings.TrimPrefix(note, "0x")
	decoded, err := hex.DecodeString(raw)
	if err != nil {
		// TronGrid returns note (a protobuf bytes field) as hex regardless of
		// visible mode. Accept plain text too for compatible FullNode providers.
		return note
	}
	return string(decoded)
}

func isStake2InternalNote(note string) bool {
	for _, prefix := range []string{
		"freezeBalanceV2For",
		"unfreezeBalanceV2For",
		"withdrawExpireUnfreeze",
		"delegateResourceOf",
		"unDelegateResourceOf",
	} {
		if strings.HasPrefix(note, prefix) {
			return true
		}
	}
	return note == "cancelAllUnfreezeV2"
}

func appendInternalAddress(addresses *[]string, seen map[string]struct{}, hexAddress string) {
	if hexAddress == "" {
		return
	}
	address := common.EncodeToBase58(hexAddress)
	if address == "" {
		return
	}
	if _, exists := seen[address]; exists {
		return
	}
	seen[address] = struct{}{}
	*addresses = append(*addresses, address)
}

func formatSuicideStake2Alert(activity suicideStake2InternalActivity) net.SlackMessage {
	fields := []activityField{
		{Label: "Risk Level", Value: "`HIGH`"},
		{Label: "Block / Index", Value: fmt.Sprintf("`%d / %d`", activity.Height, activity.Index)},
	}

	return net.SlackMessage{
		Text: fmt.Sprintf("TRON high-risk on-chain transaction detected: %s", activity.TxID),
		Blocks: alertBlocks(
			"TRON High-risk Transaction Alert",
			slackFields(fields),
			contextElements(activity.TxID, ""),
		),
	}
}

func formatSuicideStake2AIOpsAlert(activity suicideStake2InternalActivity) net.AIOpsAlert {
	content := fmt.Sprintf(
		"TRON 检测到高风险链上交易；区块/索引：%d/%d；交易哈希：%s",
		activity.Height,
		activity.Index,
		activity.TxID,
	)
	content = truncateRunes(content, 800)

	return net.AIOpsAlert{
		EventID:      activity.TxID,
		EventType:    "trigger",
		AlarmName:    "TRON 高风险链上交易告警",
		AlarmContent: content,
		EntityName:   "tron-tracker",
		EntityID:     "tron-transaction-" + activity.TxID,
		Priority:     5,
		Service:      "tron-mainnet",
		Contexts: []net.AIOpsContext{
			{Type: "link", Text: "Tronscan 交易详情", Href: "https://tronscan.io/#/transaction/" + activity.TxID},
		},
	}
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit == 1 {
		return "…"
	}
	return string(runes[:limit-1]) + "…"
}
