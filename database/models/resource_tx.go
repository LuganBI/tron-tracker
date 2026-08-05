package models

// ResourceTransaction is the compact read model used by /top_delegate and
// /top_stake. It stores every stake/unstake row, but only the daily top delegate
// rows for each of types 57/157/58/158. The canonical, wide transaction row
// remains in transactions_YYMMDD.
//
// Amount is numeric here (unlike Transaction.Amount, which must also hold token
// values wider than uint64 and legacy "<nil>" values). Resource contract amounts
// fit in uint64, allowing MySQL to rank them directly from a compact index.
type ResourceTransaction struct {
	TxDate string `gorm:"column:tx_date;size:6;primaryKey;autoIncrement:false;index:idx_resource_date_type_amount,priority:1;index:idx_resource_type_date,priority:2"`
	Height uint   `gorm:"primaryKey;autoIncrement:false"`
	Index  uint16 `gorm:"column:tx_index;primaryKey;autoIncrement:false"`

	Type      uint8  `gorm:"not null;index:idx_resource_date_type_amount,priority:2;index:idx_resource_type_date,priority:1"`
	OwnerAddr string `gorm:"size:34;not null"`
	ToAddr    string `gorm:"size:34;not null"`
	Amount    uint64 `gorm:"type:bigint unsigned;not null;index:idx_resource_date_type_amount,priority:3,sort:desc"`
}

// ResourceTransactionDay records that a finalized daily transaction table has
// been projected into ResourceTransaction. Its presence makes switching reads
// to the compact table safe; absent dates continue to use the canonical table.
type ResourceTransactionDay struct {
	TxDate string `gorm:"column:tx_date;size:6;primaryKey"`
}
