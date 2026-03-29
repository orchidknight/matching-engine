package models

import (
	"github.com/shopspring/decimal"
)

type Snapshot struct {
	Symbol Symbol               `json:"symbol"`
	Bids   [][2]decimal.Decimal `json:"bids"`
	Asks   [][2]decimal.Decimal `json:"asks"`
}
