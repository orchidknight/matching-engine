package models

import (
	"fmt"
	"github.com/google/uuid"
	"time"

	"github.com/shopspring/decimal"
)

type TradeStatus string

func (ts TradeStatus) String() string {
	return string(ts)
}

const (
	TradeStatusUnspecified = "Unspecified"
	TradeStatusNew         = "New"
	TradeStatusExecuted    = "Executed"
	TradeStatusError       = "Error"
)

type Trade struct {
	ID           string          `json:"ID"`
	TakerOrderID uuid.UUID       `json:"TakerOrderID"`
	TakerID      string          `json:"TakerID"`
	MakerOrderID uuid.UUID       `json:"MakerOrderID"`
	MakerID      string          `json:"MakerID"`
	Symbol       Symbol          `json:"Symbol"`
	TakerSide    Side            `json:"TakerSide"`
	Status       TradeStatus     `json:"Status"`
	Amount       decimal.Decimal `json:"Amount"`
	Price        decimal.Decimal `json:"Price"`
	CreatedAt    time.Time       `json:"CreatedAt"`
}

func (t *Trade) String() string {
	return fmt.Sprintf("Trade-%s %s Price: %v Amount: %v",
		t.ID, t.Symbol, t.Price, t.Amount)
}
