package database

import (
	"fmt"
	"time"
)

// Aggregated daily statistics pushed down to MySQL. These back the
// /token_amount_stats, /type_fee_stats and /addr_activity_stats endpoints and
// replace ad-hoc /q raw queries with the same semantics. Every method operates
// on a single UTC day table and returns aggregate rows only — the per-day
// transaction tables are never streamed into Go memory.

// TokenAmountBucket is one LENGTH(amount) bucket of a token's transfers for one
// day. digit_len N (N>=2) covers raw amounts in [10^(N-1), 10^N); digit_len 1
// covers 0-9. Amount is a decimal string: a full-day SUM over CAST(amount AS
// DECIMAL) can exceed comfortable JSON integer range.
type TokenAmountBucket struct {
	DigitLen    int    `json:"digit_len"`
	TxCount     int64  `json:"tx_count"`
	Fee         int64  `json:"fee"`
	StakeEnergy int64  `json:"stake_energy"`
	Amount      string `json:"amount"`
}

// GetTokenAmountBucketsByDate groups one day's transfers of the given token
// contract by LENGTH(amount), returning per-bucket count, burned fee (sun),
// stake-covered energy (energy_usage + energy_origin_usage) and raw amount sum.
// Mirrors the token statistic filters: TriggerSmartContract only (type 31 and
// its +100 energy variant) with a non-empty sender.
func (db *RawDB) GetTokenAmountBucketsByDate(date time.Time, tokenAddr string) ([]TokenAmountBucket, error) {
	table := "transactions_" + date.Format("060102")
	var buckets []TokenAmountBucket
	err := db.db.Raw(fmt.Sprintf(`
		SELECT LENGTH(amount)                                            AS digit_len,
		       COUNT(*)                                                  AS tx_count,
		       COALESCE(SUM(fee), 0)                                     AS fee,
		       COALESCE(SUM(energy_usage + energy_origin_usage), 0)      AS stake_energy,
		       CAST(COALESCE(SUM(CAST(amount AS DECIMAL(38,0))), 0) AS CHAR) AS amount
		FROM %s
		WHERE name = ? AND type IN (31, 131) AND from_addr <> ''
		GROUP BY LENGTH(amount)
		ORDER BY digit_len`, table), tokenAddr).Scan(&buckets).Error
	return buckets, err
}

// TypeFeeStat is one transaction-type row of a day's fee breakdown. Type is the
// raw stored code (energy-resource variants carry base+100, e.g. 131 for 31);
// callers merge by type%100 when rendering.
type TypeFeeStat struct {
	Type    int   `json:"type"`
	TxCount int64 `json:"tx_count"`
	Fee     int64 `json:"fee"`
}

// GetTypeFeeStatsByDate groups one day's full transaction table by type,
// returning per-type count and burned fee (sun). The per-type fee sum equals
// the day's total_statistics fee by construction (same source rows).
func (db *RawDB) GetTypeFeeStatsByDate(date time.Time) ([]TypeFeeStat, error) {
	table := "transactions_" + date.Format("060102")
	var stats []TypeFeeStat
	err := db.db.Raw(fmt.Sprintf(`
		SELECT type, COUNT(*) AS tx_count, COALESCE(SUM(fee), 0) AS fee
		FROM %s
		GROUP BY type
		ORDER BY type`, table)).Scan(&stats).Error
	return stats, err
}

// AddrActivityStat is one day's address-activity scalar set.
type AddrActivityStat struct {
	FromAddrCount      int64  `json:"from_addr_count"`
	AddrTxMedian       int64  `json:"addr_tx_median"`
	ChargerActiveCount int64  `json:"charger_active_count"`
	TrxTransferCount   int64  `json:"trx_transfer_count"`
	TrxAmount          string `json:"trx_amount"`
}

// GetAddrActivityByDate computes one day's address-activity scalars:
//   - from_addr_count: distinct originating addresses (from_stats rows)
//   - addr_tx_median: exact median of per-address daily tx counts
//     (ORDER BY tx_total OFFSET n/2 — n is in the millions, so this stays in SQL)
//   - charger_active_count: distinct active addresses known as exchange
//     chargers (fake chargers excluded)
//   - trx_transfer_count / trx_amount: native TRX transfers (type 1) count and
//     raw sun sum (decimal string)
func (db *RawDB) GetAddrActivityByDate(date time.Time) (*AddrActivityStat, error) {
	day := date.Format("060102")
	fromStats := "from_stats_" + day
	txTable := "transactions_" + day
	stat := &AddrActivityStat{}

	if err := db.db.Raw(fmt.Sprintf(
		`SELECT COUNT(*) FROM %s WHERE address <> 'total'`, fromStats)).
		Scan(&stat.FromAddrCount).Error; err != nil {
		return nil, err
	}

	if stat.FromAddrCount > 0 {
		if err := db.db.Raw(fmt.Sprintf(
			`SELECT tx_total FROM %s WHERE address <> 'total' ORDER BY tx_total LIMIT 1 OFFSET %d`,
			fromStats, stat.FromAddrCount/2)).
			Scan(&stat.AddrTxMedian).Error; err != nil {
			return nil, err
		}
	}

	if err := db.db.Raw(fmt.Sprintf(`
		SELECT COUNT(DISTINCT f.address)
		FROM %s f JOIN chargers c ON f.address = c.address
		WHERE f.address <> 'total' AND (c.is_fake = 0 OR c.is_fake IS NULL)`, fromStats)).
		Scan(&stat.ChargerActiveCount).Error; err != nil {
		return nil, err
	}

	var trx struct {
		TrxTransferCount int64
		TrxAmount        string
	}
	if err := db.db.Raw(fmt.Sprintf(`
		SELECT COUNT(*)                                                      AS trx_transfer_count,
		       CAST(COALESCE(SUM(CAST(amount AS DECIMAL(38,0))), 0) AS CHAR) AS trx_amount
		FROM %s WHERE type = 1`, txTable)).
		Scan(&trx).Error; err != nil {
		return nil, err
	}
	stat.TrxTransferCount = trx.TrxTransferCount
	stat.TrxAmount = trx.TrxAmount

	return stat, nil
}

// CollectEnergyProvider is one (exchange, provider) row of a day's energy
// delegation inflow to exchange charger addresses. DelegatedAmount is raw sun
// as a decimal string (per-provider daily sums exceed comfortable JSON ints).
type CollectEnergyProvider struct {
	Exchange        string `json:"exchange"`
	Provider        string `json:"provider"`
	TxCount         int64  `json:"tx_count"`
	DelegatedAmount string `json:"delegated_amount"`
}

// GetCollectEnergyProvidersByDate aggregates one day's ENERGY resource
// delegations (type 157, provider in owner_addr) that target known exchange
// charger addresses (fake chargers excluded), grouped by exchange and
// provider. This answers "who supplies the energy behind each exchange's
// collect sweeps": chargers hold no stake of their own, so collect energy
// arrives almost entirely through these just-in-time delegations.
func (db *RawDB) GetCollectEnergyProvidersByDate(date time.Time) ([]CollectEnergyProvider, error) {
	table := "transactions_" + date.Format("060102")
	var providers []CollectEnergyProvider
	err := db.db.Raw(fmt.Sprintf(`
		SELECT c.exchange_name                                               AS exchange,
		       t.owner_addr                                                  AS provider,
		       COUNT(*)                                                      AS tx_count,
		       CAST(COALESCE(SUM(CAST(t.amount AS DECIMAL(38,0))), 0) AS CHAR) AS delegated_amount
		FROM %s t
		JOIN chargers c ON t.to_addr = c.address
		WHERE t.type = 157 AND (c.is_fake = 0 OR c.is_fake IS NULL)
		GROUP BY c.exchange_name, t.owner_addr
		ORDER BY exchange, SUM(CAST(t.amount AS DECIMAL(38,0))) DESC`, table)).
		Scan(&providers).Error
	return providers, err
}
