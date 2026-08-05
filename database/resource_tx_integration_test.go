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
		itestStakeTx("resource", 157, 123),
		itestStakeTx("ordinary", 1, 456),
	}); err != nil {
		t.Fatalf("SaveTransactions: %v", err)
	}

	if got := countTable(t, db, "transactions_260301"); got != 2 {
		t.Fatalf("canonical row count = %d, want 2", got)
	}
	if got := countTable(t, db, "resource_transactions"); got != 1 {
		t.Fatalf("resource row count = %d, want 1", got)
	}
	if db.isResourceTransactionDayComplete("260301") {
		t.Fatal("active day must not be marked complete before finalization")
	}
}
