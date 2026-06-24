package models

import (
	"strings"
	"testing"

	"github.com/shopspring/decimal"
)

func TestNewOrderParsesDecimalStringsExactly(t *testing.T) {
	order, err := NewOrder(NewOrderRequest{
		User:            "user",
		Symbol:          "BTC-USDT",
		Type:            OrderTypeLimit.String(),
		Side:            "buy",
		ActivationPrice: "0.000000000000000001",
		Price:           "0.1",
		Size:            "0.2",
		Amount:          "0.3",
	})
	if err != nil {
		t.Fatalf("NewOrder() error = %v", err)
	}

	assertDecimalEqual(t, order.Price, "0.1")
	assertDecimalEqual(t, order.OriginalTotal, "0.2")
	assertDecimalEqual(t, order.AvailableTotal, "0.2")
	assertDecimalEqual(t, order.OriginalAmount, "0.3")
	assertDecimalEqual(t, order.AvailableAmount, "0.3")
	assertDecimalEqual(t, order.ActivationPrice, "0.000000000000000001")
}

func TestNewOrderRejectsInvalidDecimalString(t *testing.T) {
	_, err := NewOrder(NewOrderRequest{
		User:            "user",
		Symbol:          "BTC-USDT",
		Type:            OrderTypeLimit.String(),
		Side:            "buy",
		ActivationPrice: "0",
		Price:           "abc",
		Size:            "0.2",
		Amount:          "0.3",
	})
	if err == nil {
		t.Fatal("NewOrder() error = nil, want invalid price error")
	}
	if !strings.Contains(err.Error(), "price") || !strings.Contains(err.Error(), "abc") {
		t.Fatalf("NewOrder() error = %q, want field name and invalid value", err)
	}
}

func TestApplyUpdateDoesNotMutateInput(t *testing.T) {
	order := &Order{
		ExecutedTotal: decimal.NewFromInt(50),
	}
	update := &OrderUpdate{
		Status:         OrderStatusPartiallyCompleted,
		ExecutedAmount: decimal.NewFromInt(2),
		ExecutedTotal:  decimal.NewFromInt(200),
	}

	order.ApplyUpdate(update)

	if !update.ExecutedTotal.Equal(decimal.NewFromInt(200)) {
		t.Fatalf("update.ExecutedTotal mutated to %s, want 200", update.ExecutedTotal)
	}
	if !update.AvgPrice.Equal(decimal.Zero) {
		t.Fatalf("update.AvgPrice mutated to %s, want 0", update.AvgPrice)
	}
}

func TestApplyUpdateComputesOrderState(t *testing.T) {
	order := &Order{
		ExecutedTotal: decimal.NewFromInt(50),
	}
	update := &OrderUpdate{
		Status:         OrderStatusPartiallyCompleted,
		ExecutedAmount: decimal.NewFromInt(5),
		ExecutedTotal:  decimal.NewFromInt(200),
	}

	order.ApplyUpdate(update)

	if order.Status != OrderStatusPartiallyCompleted {
		t.Fatalf("order.Status = %s, want %s", order.Status, OrderStatusPartiallyCompleted)
	}
	// ExecutedTotal accumulates: 50 + 200 = 250; AvgPrice = 250 / 5 = 50.
	assertDecimalEqual(t, order.ExecutedTotal, "250")
	assertDecimalEqual(t, order.AvgPrice, "50")
}

func assertDecimalEqual(t *testing.T, got decimal.Decimal, want string) {
	t.Helper()

	wantDecimal, err := decimal.NewFromString(want)
	if err != nil {
		t.Fatalf("decimal.NewFromString(%q) error = %v", want, err)
	}
	if !got.Equal(wantDecimal) {
		t.Fatalf("decimal = %s, want %s", got, wantDecimal)
	}
}
