package engine

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/orchidknight/matching-engine/models"
)

func TestOrderbookEnoughLiquidityUsesInsertedVolume(t *testing.T) {
	tests := map[string]struct {
		makerSide   models.Side
		takerSide   models.Side
		makerAmount decimal.Decimal
		takerAmount decimal.Decimal
		want        bool
	}{
		"market sell uses bid volume": {
			makerSide:   models.Buy,
			takerSide:   models.Sell,
			makerAmount: decimal.NewFromInt(5),
			takerAmount: decimal.NewFromInt(3),
			want:        true,
		},
		"market buy uses ask volume": {
			makerSide:   models.Sell,
			takerSide:   models.Buy,
			makerAmount: decimal.NewFromInt(5),
			takerAmount: decimal.NewFromInt(3),
			want:        true,
		},
		"market sell rejects amount above bid volume": {
			makerSide:   models.Buy,
			takerSide:   models.Sell,
			makerAmount: decimal.NewFromInt(5),
			takerAmount: decimal.NewFromInt(6),
			want:        false,
		},
		"market buy rejects amount above ask volume": {
			makerSide:   models.Sell,
			takerSide:   models.Buy,
			makerAmount: decimal.NewFromInt(5),
			takerAmount: decimal.NewFromInt(6),
			want:        false,
		},
	}

	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			testBook := newTestOrderbook()
			makerOrder := newRestingOrder(testCase.makerSide, testCase.makerAmount)
			takerOrder := newMarketOrder(testCase.takerSide, testCase.takerAmount)

			if err := testBook.InsertOrder(makerOrder); err != nil {
				t.Fatalf("InsertOrder() error = %v", err)
			}

			got := testBook.EnoughLiquidity(takerOrder)
			if got != testCase.want {
				t.Fatalf("EnoughLiquidity() = %v, want %v", got, testCase.want)
			}
		})
	}
}

func newTestOrderbook() *Orderbook {
	return NewOrderbook(
		models.Symbol("BTC-USDT"),
		NewOrderbookMock(),
		NewOrdersMock(),
		NewMarketsMock(),
		NewTradesMock(),
		NewLogMock(),
	)
}

func newRestingOrder(side models.Side, amount decimal.Decimal) *models.Order {
	return &models.Order{
		ID:              uuid.New(),
		Account:         "maker",
		Symbol:          "BTC-USDT",
		Type:            models.OrderTypeLimit,
		Status:          models.OrderStatusOpen,
		Side:            side,
		AvailableAmount: amount,
		Price:           decimal.NewFromInt(100),
	}
}

func newMarketOrder(side models.Side, amount decimal.Decimal) *models.Order {
	return &models.Order{
		ID:              uuid.New(),
		Account:         "taker",
		Symbol:          "BTC-USDT",
		Type:            models.OrderTypeMarket,
		Status:          models.OrderStatusNew,
		Side:            side,
		AvailableAmount: amount,
	}
}
