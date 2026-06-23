package engine

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/orchidknight/matching-engine/models"
)

func TestPriceNodeGetOrderByUUID(t *testing.T) {
	priceNode := newPriceNode(decimal.NewFromInt(100))
	order := newRestingOrder(models.Buy, decimal.NewFromInt(3))

	if err := priceNode.InsertOrder(*order); err != nil {
		t.Fatalf("InsertOrder() error = %v", err)
	}

	got, exists := priceNode.GetOrder(order.ID)
	if !exists {
		t.Fatal("GetOrder() exists = false, want true")
	}
	if got.ID != order.ID {
		t.Fatalf("GetOrder() ID = %s, want %s", got.ID, order.ID)
	}

	_, exists = priceNode.GetOrder(uuid.New())
	if exists {
		t.Fatal("GetOrder() exists = true for unknown ID, want false")
	}
}

func TestOrderbookGetOrderByUUID(t *testing.T) {
	testBook := newTestOrderbook()
	order := newRestingOrder(models.Sell, decimal.NewFromInt(3))

	if err := testBook.InsertOrder(order); err != nil {
		t.Fatalf("InsertOrder() error = %v", err)
	}

	got, exists := testBook.GetOrder(order.ID, "sell", order.Price)
	if !exists {
		t.Fatal("GetOrder() exists = false, want true")
	}
	if got.ID != order.ID {
		t.Fatalf("GetOrder() ID = %s, want %s", got.ID, order.ID)
	}

	_, exists = testBook.GetOrder(uuid.New(), "sell", order.Price)
	if exists {
		t.Fatal("GetOrder() exists = true for unknown ID, want false")
	}
}
