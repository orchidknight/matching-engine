package models

import (
	"fmt"

	"github.com/shopspring/decimal"
)

type Price struct {
	Symbol    Symbol
	LastPrice decimal.Decimal
}

func (p Price) String() string {
	return fmt.Sprintf("Price %s %v", p.Symbol, p.LastPrice)
}
