package tron

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

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

type suicideContractStatistic struct {
	CalledContract string
	TxCount        int64
	SuicideCount   int64
}

// ReportSuicideWithStake2 accumulates successful SUICIDE internals for the
// daily summary. It reports immediately only when the same transaction also
// contains a successful Stake 2.0-related internal transaction.
func (m *ActivityMonitor) ReportSuicideWithStake2(
	internalTxs []types.InternalTx, height uint, index uint16, txID, calledContract string,
) {
	if m == nil {
		return
	}

	activity, ok := detectSuicideWithStake2(internalTxs, height, index, txID)
	if activity.SuicideCount > 0 {
		m.addSuicideContractStatistic(calledContract, activity.SuicideCount)
	}
	if !ok {
		return
	}

	net.ReportWarningChannelMessageToSlack(formatSuicideStake2Alert(activity))
	if err := net.ReportAIOpsAlert(m.aiopsAppKeys, formatSuicideStake2AIOpsAlert(activity)); err != nil {
		zap.S().Errorf("report high-risk transaction alert to AIOps failed: %v", err)
	}
}

func (t *Tracker) ReportSuicideInternalSummary(end time.Time) error {
	if t == nil || t.activityMonitor == nil {
		return nil
	}

	start := end.Add(-24 * time.Hour)
	stats := t.activityMonitor.takeSuicideContractStatistics()

	net.ReportWarningMessageToSlack(net.SlackMessage{
		Text: formatSuicideInternalSummary(start, end, stats),
	})
	return nil
}

func (m *ActivityMonitor) addSuicideContractStatistic(contract string, suicideCount int) {
	m.suicideSummaryMu.Lock()
	defer m.suicideSummaryMu.Unlock()

	stat := m.suicideSummaryByContract[contract]
	if stat == nil {
		stat = &suicideContractStatistic{CalledContract: contract}
		m.suicideSummaryByContract[contract] = stat
	}
	stat.TxCount++
	stat.SuicideCount += int64(suicideCount)
}

func (m *ActivityMonitor) takeSuicideContractStatistics() []suicideContractStatistic {
	m.suicideSummaryMu.Lock()
	defer m.suicideSummaryMu.Unlock()

	stats := make([]suicideContractStatistic, 0, len(m.suicideSummaryByContract))
	for _, stat := range m.suicideSummaryByContract {
		stats = append(stats, *stat)
	}
	m.suicideSummaryByContract = make(map[string]*suicideContractStatistic)
	return stats
}

func formatSuicideInternalSummary(
	start, end time.Time, stats []suicideContractStatistic,
) string {
	stats = append([]suicideContractStatistic(nil), stats...)
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].TxCount != stats[j].TxCount {
			return stats[i].TxCount > stats[j].TxCount
		}
		return stats[i].CalledContract < stats[j].CalledContract
	})

	var totalTxs, totalInternals int64
	for _, stat := range stats {
		totalTxs += stat.TxCount
		totalInternals += stat.SuicideCount
	}

	zone := end.Format("MST")
	var b strings.Builder
	fmt.Fprintf(&b,
		"TRON internal transaction daily summary\nPeriod: `%s` - `%s` (%s)\nTransactions: `%d`\nInternal activities: `%d`\nCalled contracts: `%d`",
		start.Format("2006-01-02 15:04:05"),
		end.Format("2006-01-02 15:04:05"),
		zone,
		totalTxs,
		totalInternals,
		len(stats),
	)

	for i, stat := range stats {
		contract := stat.CalledContract
		if contract == "" {
			contract = "unknown"
		}
		percentage := float64(0)
		if totalTxs > 0 {
			percentage = float64(stat.TxCount) * 100 / float64(totalTxs)
		}
		fmt.Fprintf(&b, "\n%d. `%s`: `%d` tx (%.2f%%), `%d` internal",
			i+1, contract, stat.TxCount, percentage, stat.SuicideCount)
	}

	return truncateRunes(b.String(), 35_000)
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
