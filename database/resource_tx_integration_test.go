//go:build integration

package database

import (
	"testing"
	"time"

	"tron-tracker/database/models"
)

func TestResourceBackfillSwitchesRankingsToNarrowReadModel(t *testing.T) {
	db := newFlushTestDB(t)
	date := time.Date(2026, 2, 11, 0, 0, 0, 0, time.Local)
	txs := []*models.Transaction{
		itestStakeTx("delegate-small", 57, 100),
		itestStakeTx("delegate-large", 157, 900),
		itestStakeTx("undelegate", 58, 800),
		itestStakeTx("staker", 54, 700),
		itestStakeTx("ignored", 255, 999),
	}
	for i, tx := range txs {
		tx.Height = uint(i + 1)
	}
	seedTxs(t, db, "260211", txs)

	if err := db.BackfillResourceTransactions(date, 1); err != nil {
		t.Fatalf("BackfillResourceTransactions: %v", err)
	}
	if !db.isResourceTransactionDayComplete("260211") {
		t.Fatal("backfilled day was not marked complete")
	}
	if got := countTable(t, db, "resource_transactions"); got != 4 {
		t.Fatalf("resource row count = %d, want 4", got)
	}
	if !indexExists(t, db, "resource_transactions", "idx_resource_date_type_amount") {
		t.Fatal("missing delegate ranking index")
	}
	if !indexExists(t, db, "resource_transactions", "idx_resource_type_date") {
		t.Fatal("missing stake range index")
	}

	// Removing the canonical table proves both reads use the completed narrow
	// model rather than merely returning the same result via the fallback path.
	if err := db.db.Migrator().DropTable("transactions_260211"); err != nil {
		t.Fatalf("drop canonical table: %v", err)
	}

	delegates := db.GetTopDelegateRelatedTxsByDateAndN(date, 10, false)
	if len(delegates) != 2 || delegates[0].OwnerAddr != "delegate-large" || delegates[1].OwnerAddr != "delegate-small" {
		t.Fatalf("delegate ranking from narrow model = %#v", delegates)
	}

	stakeTxs := db.GetStakeRelatedTxsByDateDays(date, 1)
	if len(stakeTxs) != 1 || stakeTxs[0].OwnerAddr != "staker" || stakeTxs[0].Amount.String() != "700" {
		t.Fatalf("stake rows from narrow model = %#v", stakeTxs)
	}
}

func TestSaveTransactionsWritesResourceReadModelAtomically(t *testing.T) {
	db := newFlushTestDB(t)
	db.trackingDate = "260301"

	if err := db.SaveTransactions([]*models.Transaction{
		itestStakeTx("stake", 54, 789),
		itestStakeTx("delegate", 157, 123),
		itestStakeTx("ordinary", 1, 456),
	}); err != nil {
		t.Fatalf("SaveTransactions: %v", err)
	}

	if got := countTable(t, db, "transactions_260301"); got != 3 {
		t.Fatalf("canonical row count = %d, want 3", got)
	}
	if got := countTable(t, db, "resource_transactions"); got != 1 {
		t.Fatalf("resource row count = %d, want 1", got)
	}
	var stored models.ResourceTransaction
	if err := db.db.First(&stored).Error; err != nil {
		t.Fatalf("read compact row: %v", err)
	}
	if stored.Type != 54 || stored.OwnerAddr != "stake" {
		t.Fatalf("live compact row = %#v, want stake only", stored)
	}
	if db.isResourceTransactionDayComplete("260301") {
		t.Fatal("active day must not be marked complete before finalization")
	}
	// An unfinalized day still ranks delegates from the canonical table.
	got := db.GetTopDelegateRelatedTxsByDateAndN(time.Date(2026, 3, 1, 0, 0, 0, 0, time.Local), 10, false)
	if len(got) != 1 || got[0].OwnerAddr != "delegate" {
		t.Fatalf("active-day delegate fallback = %#v", got)
	}
}

func TestResourceBackfillCompactsDelegateRowsPerType(t *testing.T) {
	db := newFlushTestDB(t)
	date := time.Date(2026, 2, 11, 0, 0, 0, 0, time.Local)

	canonical := make([]*models.Transaction, 0, 2010)
	legacy := make([]*models.ResourceTransaction, 0, 2008)
	var height uint
	addDelegates := func(txType uint8, count int, tied bool) {
		for i := 0; i < count; i++ {
			height++
			amount := int64(i + 1)
			if tied {
				amount = 500
			}
			tx := itestStakeTx("delegate", txType, amount)
			tx.Height = height
			canonical = append(canonical, tx)
			legacy = append(legacy, &models.ResourceTransaction{
				TxDate:    "260211",
				Height:    height,
				Type:      txType,
				OwnerAddr: tx.OwnerAddr,
				Amount:    uint64(amount),
			})
		}
	}
	addDelegates(57, 1005, true)
	addDelegates(157, 1003, false)
	height++
	stake := itestStakeTx("stake", 54, 42)
	stake.Height = height
	canonical = append(canonical, stake)
	seedTxs(t, db, "260211", canonical)

	// Mimic a date built by the original implementation, which copied every
	// delegate row and already had a completion marker.
	if err := db.db.CreateInBatches(legacy, 500).Error; err != nil {
		t.Fatalf("seed legacy resource rows: %v", err)
	}
	if err := db.db.Create(&models.ResourceTransactionDay{TxDate: "260211"}).Error; err != nil {
		t.Fatalf("seed completion marker: %v", err)
	}

	for run := 1; run <= 2; run++ {
		if err := db.BackfillResourceTransactions(date, 1); err != nil {
			t.Fatalf("backfill run %d: %v", run, err)
		}
		if got := countTable(t, db, "resource_transactions"); got != 2001 {
			t.Fatalf("run %d compact row count = %d, want 2001", run, got)
		}
	}

	for _, test := range []struct {
		txType uint8
		want   int64
	}{
		{txType: 57, want: DelegateTopPerType},
		{txType: 157, want: DelegateTopPerType},
		{txType: 54, want: 1},
	} {
		var got int64
		if err := db.db.Model(&models.ResourceTransaction{}).
			Where("tx_date = ? AND type = ?", "260211", test.txType).
			Count(&got).Error; err != nil {
			t.Fatalf("count type %d: %v", test.txType, err)
		}
		if got != test.want {
			t.Fatalf("type %d count = %d, want %d", test.txType, got, test.want)
		}
	}

	var minEnergyAmount uint64
	if err := db.db.Model(&models.ResourceTransaction{}).
		Where("tx_date = ? AND type = ?", "260211", 157).
		Select("MIN(amount)").Scan(&minEnergyAmount).Error; err != nil {
		t.Fatalf("minimum retained energy amount: %v", err)
	}
	if minEnergyAmount != 4 {
		t.Fatalf("minimum retained type-157 amount = %d, want 4", minEnergyAmount)
	}

	got := db.GetTopDelegateRelatedTxsByDateAndN(date, DelegateTopPerType, false)
	if len(got) != DelegateTopPerType {
		t.Fatalf("top delegate size = %d, want %d", len(got), DelegateTopPerType)
	}
	if got[0].Type != 157 || got[0].Amount.String() != "1003" {
		t.Fatalf("top delegate = type %d amount %s, want type 157 amount 1003", got[0].Type, got[0].Amount)
	}
}
