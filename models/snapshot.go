package models

import (
	"github.com/shopspring/decimal"
)

type Snapshot struct {
	Symbol Symbol               `json:"s"`
	Bids   [][2]decimal.Decimal `json:"b"`
	Asks   [][2]decimal.Decimal `json:"a"`
}
