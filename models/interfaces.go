package models

import (
	"context"
	"github.com/google/uuid"

	"github.com/shopspring/decimal"
)

type TradeConsumer interface {
	ConsumeTrade(ctx context.Context, trade *Trade) error
}

type OrderConsumer interface {
	ConsumeOrder(o *Order)
}

type OrderbookService interface {
	TradeConsumer
}

type MarketService interface {
	GetMarkets() ([]*Market, error)
	UpdateMarket(market *Market) error
	GetMarketByID(id string) (*Market, error)
}

type TradeService interface {
	TradeConsumer
	LastPrice(s Symbol) decimal.Decimal
}

type EngineService interface {
	Run(ctx context.Context) error
	ConsumeOrder(order *Order)
}

type OrderService interface {
	UpdateOrder(ctx context.Context, order *Order) error
	GetOrderByID(ctx context.Context, id uuid.UUID) (*Order, error)
	GetOrdersByPair(ctx context.Context, pair string) ([]*Order, error)
	Reject(ctx context.Context, o *Order) error
}

type Logger interface {
	Debug(component string, format string, a ...any)
	Info(component string, format string, a ...any)
	Warn(component string, format string, a ...any)
	Error(component string, format string, a ...any)
	Fatal(component string, format string, a ...any)
}
