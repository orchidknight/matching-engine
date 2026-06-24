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
