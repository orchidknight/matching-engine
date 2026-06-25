package engine

import (
	"context"
	"sync"
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

// TestOrderbookEnoughLiquidityAllowsTriggeredStopMarket locks in the deliberate
// asymmetry of the guard (RF2-04): a (triggered) stop-market is never gated by
// liquidity, even when its size exceeds book depth. Such an order must reach
// Match and execute as IOC — partial fill plus canceled remainder (see RF2-03,
// TestMatchStopMarketByAmountMovesUncoveredRemainderToCanceled) — rather than be
// rejected up front, which would leave a stop-loss position unprotected.
func TestOrderbookEnoughLiquidityAllowsTriggeredStopMarket(t *testing.T) {
	tests := map[string]struct {
		makerSide models.Side
		takerSide models.Side
	}{
		"stop-market buy past ask depth":  {makerSide: models.Sell, takerSide: models.Buy},
		"stop-market sell past bid depth": {makerSide: models.Buy, takerSide: models.Sell},
	}

	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			testBook := newTestOrderbook()
			makerOrder := newRestingOrder(testCase.makerSide, decimal.NewFromInt(2))
			if err := testBook.InsertOrder(makerOrder); err != nil {
				t.Fatalf("InsertOrder() error = %v", err)
			}

			// Amount (5) deliberately out-sizes book depth (2).
			takerOrder := newMarketOrder(testCase.takerSide, decimal.NewFromInt(5))
			takerOrder.Type = models.OrderTypeStopMarket
			takerOrder.Status = models.OrderStatusTriggered

			if !testBook.EnoughLiquidity(takerOrder) {
				t.Fatal("EnoughLiquidity() = false, want true: stop-market must bypass the guard")
			}
		})
	}
}

func TestOrderbookSnapshotConcurrentAccess(t *testing.T) {
	testBook := newTestOrderbook()
	start := make(chan struct{})
	writerDone := make(chan struct{})
	snapshotWorkers := startSnapshotWorkers(testBook, start, writerDone, 4)
	defer snapshotWorkers.Wait()
	defer close(writerDone)

	close(start)

	for i := range 1000 {
		insertAndMatchSnapshotRaceOrder(t, testBook, i)
	}
}

func startSnapshotWorkers(testBook *Orderbook, start <-chan struct{}, done <-chan struct{}, count int) *sync.WaitGroup {
	var snapshotWorkers sync.WaitGroup

	for range count {
		snapshotWorkers.Add(1)
		go func() {
			defer snapshotWorkers.Done()
			<-start

			for {
				select {
				case <-done:
					return
				default:
					_ = testBook.Snapshot()
				}
			}
		}()
	}

	return &snapshotWorkers
}

func insertAndMatchSnapshotRaceOrder(t *testing.T, testBook *Orderbook, iteration int) {
	t.Helper()

	makerOrder := newRestingOrder(models.Sell, decimal.NewFromInt(1))
	makerOrder.Price = decimal.NewFromInt(int64(100 + iteration%10))

	if err := testBook.InsertOrder(makerOrder); err != nil {
		t.Fatalf("InsertOrder() error = %v", err)
	}

	takerOrder := newLimitOrder(models.Buy, decimal.NewFromInt(1), decimal.NewFromInt(200))
	response, err := testBook.Match(context.Background(), takerOrder)
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}
	if response.InitialOrder == nil {
		t.Fatal("Match().InitialOrder is nil")
	}
	if len(response.MatchedOrders) != 1 {
		t.Fatalf("len(Match().MatchedOrders) = %d, want 1", len(response.MatchedOrders))
	}
}

func newTestOrderbook() *Orderbook {
	return NewOrderbook(
		models.Symbol("BTC-USDT"),
		defaultBaseScale,
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
