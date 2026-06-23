package engine

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/orchidknight/matching-engine/models"
)

func TestMatchMarketableLimitWithOnlySelfLiquidityRestsOrder(t *testing.T) {
	testBook := newTestOrderbook()
	restingAsk := newRestingOrder(models.Sell, decimal.NewFromInt(5))
	restingAsk.Account = "account-1"

	if err := testBook.InsertOrder(restingAsk); err != nil {
		t.Fatalf("InsertOrder() error = %v", err)
	}

	takerOrder := newLimitOrder(models.Buy, decimal.NewFromInt(3), decimal.NewFromInt(100))
	takerOrder.Account = restingAsk.Account

	if !testBook.CanMatchImmediately(takerOrder) {
		t.Fatal("CanMatchImmediately() = false, want true")
	}

	response := testBook.Match(context.Background(), takerOrder)

	if response.InitialOrder == nil {
		t.Fatal("Match().InitialOrder is nil")
	}
	if len(response.MatchedOrders) != 0 {
		t.Fatalf("len(Match().MatchedOrders) = %d, want 0", len(response.MatchedOrders))
	}
	if response.InitialOrder.Status != models.OrderStatusOpen {
		t.Fatalf("Match().InitialOrder.Status = %s, want %s", response.InitialOrder.Status, models.OrderStatusOpen)
	}
	if !response.InitialOrder.AvailableAmount.Equal(takerOrder.AvailableAmount) {
		t.Fatalf("Match().InitialOrder.AvailableAmount = %s, want %s", response.InitialOrder.AvailableAmount, takerOrder.AvailableAmount)
	}
	if !response.InitialOrder.ExecutedAmount.Equal(Zero) {
		t.Fatalf("Match().InitialOrder.ExecutedAmount = %s, want %s", response.InitialOrder.ExecutedAmount, Zero)
	}

	priceItem := testBook.bidsTree.Get(newPriceNode(takerOrder.Price))
	if priceItem == nil {
		t.Fatalf("bid price level %s was not inserted", takerOrder.Price)
	}
	if _, exists := priceItem.(*PriceNode).orderMap.Get(takerOrder.ID); !exists {
		t.Fatalf("taker order %s was not inserted into bids", takerOrder.ID)
	}
}

func newLimitOrder(side models.Side, amount decimal.Decimal, price decimal.Decimal) *models.Order {
	return &models.Order{
		ID:              uuid.New(),
		Account:         "taker",
		Symbol:          "BTC-USDT",
		Type:            models.OrderTypeLimit,
		Status:          models.OrderStatusNew,
		Side:            side,
		AvailableAmount: amount,
		Price:           price,
	}
}
