package models

// ResourceTransaction is the narrow read model used by /top_delegate and
// /top_stake. The canonical, wide transaction row remains in the daily
// transactions_YYMMDD table; this table only duplicates the fields required by
// the resource ranking endpoints.
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

// ResourceTransactionDay records that a daily wide transaction table has been
// fully copied into ResourceTransaction. Its presence makes switching reads to
// the narrow table safe; absent dates continue to use the legacy daily table.
type ResourceTransactionDay struct {
	TxDate string `gorm:"column:tx_date;size:6;primaryKey"`
}
