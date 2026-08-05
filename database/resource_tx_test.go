package database

import "testing"

func TestIsResourceTransactionTypeUsesExactContractTypes(t *testing.T) {
	for _, txType := range []uint8{12, 112, 54, 154, 55, 155, 57, 157, 58, 158, 59, 159} {
		if !isResourceTransactionType(txType) {
			t.Errorf("type %d should be copied to the resource read model", txType)
		}
	}
	// TransferType is 255; modulo-based matching would incorrectly classify it
	// as UnfreezeBalanceV2 because 255 %% 100 == 55.
	for _, txType := range []uint8{0, 1, 31, 111, 255} {
		if isResourceTransactionType(txType) {
			t.Errorf("type %d must stay out of the resource read model", txType)
		}
	}
}
