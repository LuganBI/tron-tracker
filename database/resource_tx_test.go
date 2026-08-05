package database

import "testing"

func TestIsStakeTransactionTypeUsesExactContractTypes(t *testing.T) {
	for _, txType := range []uint8{12, 112, 54, 154, 55, 155, 59, 159} {
		if !isStakeTransactionType(txType) {
			t.Errorf("type %d should be copied to the live stake read model", txType)
		}
	}
	// Delegate/undelegate rows are ranked and copied only when the day is
	// finalized; retaining all of them during ingestion defeats compaction.
	// TransferType is 255; modulo-based matching would incorrectly classify it
	// as UnfreezeBalanceV2 because 255 %% 100 == 55.
	for _, txType := range []uint8{0, 1, 31, 57, 157, 58, 158, 111, 255} {
		if isStakeTransactionType(txType) {
			t.Errorf("type %d must stay out of the live stake read model", txType)
		}
	}
}
