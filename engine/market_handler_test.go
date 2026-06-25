package engine

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/orchidknight/matching-engine/models"
)

func TestMarketHandlerProcessOrderRejectsInvalidMarketInput(t *testing.T) {
	tests := map[string]struct {
		order      *models.Order
		wantReason models.RejectReason
	}{
		"amount below min order size": {
			order:      newValidationLimitOrder(decimal.RequireFromString("0.99"), decimal.RequireFromString("100.00")),
			wantReason: models.RejectReasonMinOrderSize,
		},
		"price not aligned to tick": {
			order:      newValidationLimitOrder(decimal.RequireFromString("1.00"), decimal.RequireFromString("100.001")),
			wantReason: models.RejectReasonInvalidPricePrecision,
		},
		"amount not aligned to lot": {
			order:      newValidationLimitOrder(decimal.RequireFromString("1.001"), decimal.RequireFromString("100.00")),
			wantReason: models.RejectReasonInvalidAmountPrecision,
		},
		"total not aligned to quote precision": {
			order:      newValidationMarketOrderWithTotal(decimal.RequireFromString("100.001")),
			wantReason: models.RejectReasonInvalidTotalPrecision,
		},
		"activation price not aligned to tick": {
			order:      newValidationStopLimitOrder(decimal.RequireFromString("90.001")),
			wantReason: models.RejectReasonInvalidPricePrecision,
		},
		"limit order without amount": {
			order:      newValidationLimitOrderWithoutAmount(models.OrderTypeLimit),
			wantReason: models.RejectReasonMissingAmount,
		},
		"stop-limit order without amount": {
			order:      newValidationLimitOrderWithoutAmount(models.OrderTypeStopLimit),
			wantReason: models.RejectReasonMissingAmount,
		},
	}

	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			marketHandler := newValidationMarketHandler(t)

			if err := marketHandler.ProcessOrder(context.Background(), testCase.order); err != nil {
				t.Fatalf("ProcessOrder() error = %v", err)
			}

			response := readOrderResponse(t, marketHandler.engine)
			gotOrder := response.InitialOrder
			if gotOrder.Status != models.OrderStatusRejected {
				t.Fatalf("order status = %s, want %s", gotOrder.Status, models.OrderStatusRejected)
			}
			if gotOrder.RejectedReason != testCase.wantReason {
				t.Fatalf("rejected reason = %s, want %s", gotOrder.RejectedReason, testCase.wantReason)
			}
		})
	}
}

func newValidationMarketHandler(t *testing.T) *MarketHandler {
	t.Helper()

	engine, err := NewEngine(
		context.Background(),
		nil,
		newRecordingOrderService(),
		&staticTradeService{lastPrice: decimal.NewFromInt(100)},
		NewOrderbookMock(),
		NewLogMock(),
	)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	marketHandler, err := NewMarketHandler(engine, &models.Market{
		ID: "BTC-USDT",
		BaseAsset: &models.Asset{
			ID:                   "BTC",
			InputPrecision:       2,
			CalculationPrecision: 8,
		},
		QuoteAsset: &models.Asset{
			ID:                   "USDT",
			InputPrecision:       2,
			CalculationPrecision: 8,
		},
		MinOrderSize: decimal.NewFromInt(1),
	})
	if err != nil {
		t.Fatalf("NewMarketHandler() error = %v", err)
	}

	return marketHandler
}

func newValidationLimitOrder(amount decimal.Decimal, price decimal.Decimal) *models.Order {
	return &models.Order{
		ID:              uuid.New(),
		Account:         "taker",
		Symbol:          "BTC-USDT",
		Type:            models.OrderTypeLimit,
		Status:          models.OrderStatusNew,
		Side:            models.Buy,
		AvailableAmount: amount,
		Price:           price,
	}
}

func newValidationMarketOrderWithTotal(total decimal.Decimal) *models.Order {
	return &models.Order{
		ID:             uuid.New(),
		Account:        "taker",
		Symbol:         "BTC-USDT",
		Type:           models.OrderTypeMarket,
		Status:         models.OrderStatusNew,
		Side:           models.Buy,
		AvailableTotal: total,
	}
}

func newValidationLimitOrderWithoutAmount(orderType models.OrderType) *models.Order {
	return &models.Order{
		ID:              uuid.New(),
		Account:         "taker",
		Symbol:          "BTC-USDT",
		Type:            orderType,
		Status:          models.OrderStatusNew,
		Side:            models.Buy,
		AvailableTotal:  decimal.NewFromInt(100),
		Price:           decimal.NewFromInt(100),
		ActivationPrice: decimal.NewFromInt(90),
	}
}

func newValidationStopLimitOrder(activationPrice decimal.Decimal) *models.Order {
	return &models.Order{
		ID:              uuid.New(),
		Account:         "taker",
		Symbol:          "BTC-USDT",
		Type:            models.OrderTypeStopLimit,
		Status:          models.OrderStatusNew,
		Side:            models.Buy,
		AvailableAmount: decimal.NewFromInt(1),
		Price:           decimal.NewFromInt(100),
		ActivationPrice: activationPrice,
	}
}

func TestMarketHandlerProcessCancelRemovesRestingOrder(t *testing.T) {
	marketHandler := newValidationMarketHandler(t)
	orders := marketHandler.engine.orders.(*recordingOrderService)

	resting := newRestingLimitOrder()
	if err := marketHandler.orderbook.InsertOrder(resting); err != nil {
		t.Fatalf("InsertOrder() error = %v", err)
	}
	if err := orders.UpdateOrder(context.Background(), resting); err != nil {
		t.Fatalf("UpdateOrder() error = %v", err)
	}

	cancel := &models.Order{
		ID:     resting.ID,
		Symbol: resting.Symbol,
		Status: models.OrderStatusPendingCancel,
	}
	if err := marketHandler.ProcessOrder(context.Background(), cancel); err != nil {
		t.Fatalf("ProcessOrder() error = %v", err)
	}

	response := readOrderResponse(t, marketHandler.engine)
	got := response.InitialOrder
	if got.ID != resting.ID {
		t.Fatalf("response order id = %s, want %s", got.ID, resting.ID)
	}
	if got.Status != models.OrderStatusCanceled {
		t.Fatalf("order status = %s, want %s", got.Status, models.OrderStatusCanceled)
	}
	if !got.AvailableAmount.Equal(decimal.Zero) {
		t.Fatalf("available amount = %s, want 0", got.AvailableAmount)
	}
	if !got.CanceledAmount.Equal(decimal.NewFromInt(2)) {
		t.Fatalf("canceled amount = %s, want 2", got.CanceledAmount)
	}

	// order.Cancel() applied and persisted
	persisted, err := orders.GetOrderByID(context.Background(), resting.ID)
	if err != nil {
		t.Fatalf("GetOrderByID() error = %v", err)
	}
	if persisted.Status != models.OrderStatusCanceled {
		t.Fatalf("persisted status = %s, want %s", persisted.Status, models.OrderStatusCanceled)
	}

	// RemoveOrder was called: the resting order is gone from the book
	snapshot := marketHandler.orderbook.Snapshot()
	if len(snapshot.Bids) != 0 {
		t.Fatalf("bids after cancel = %d, want 0", len(snapshot.Bids))
	}
}

func TestMarketHandlerProcessCancelMissingOrderNoop(t *testing.T) {
	marketHandler := newValidationMarketHandler(t)

	cancel := &models.Order{
		ID:     uuid.New(),
		Symbol: "BTC-USDT",
		Status: models.OrderStatusPendingCancel,
	}
	if err := marketHandler.ProcessOrder(context.Background(), cancel); err != nil {
		t.Fatalf("ProcessOrder() error = %v", err)
	}

	select {
	case response := <-marketHandler.engine.outcomingOrderResponses:
		t.Fatalf("unexpected order response for missing order: %+v", response)
	default:
	}
}

func TestMarketHandlerProcessCancelRemoveOrderError(t *testing.T) {
	marketHandler := newValidationMarketHandler(t)
	orders := marketHandler.engine.orders.(*recordingOrderService)

	// Order is known to the order service but never rested in the book.
	resting := newRestingLimitOrder()
	if err := orders.UpdateOrder(context.Background(), resting); err != nil {
		t.Fatalf("UpdateOrder() error = %v", err)
	}

	cancel := &models.Order{
		ID:     resting.ID,
		Symbol: resting.Symbol,
		Status: models.OrderStatusPendingCancel,
	}
	if err := marketHandler.ProcessOrder(context.Background(), cancel); err == nil {
		t.Fatal("ProcessOrder() error = nil, want RemoveOrder error")
	}
}

func newRestingLimitOrder() *models.Order {
	return &models.Order{
		ID:              uuid.New(),
		Account:         "maker",
		Symbol:          "BTC-USDT",
		Type:            models.OrderTypeLimit,
		Status:          models.OrderStatusOpen,
		Side:            models.Buy,
		OriginalAmount:  decimal.NewFromInt(2),
		AvailableAmount: decimal.NewFromInt(2),
		Price:           decimal.NewFromInt(100),
	}
}
