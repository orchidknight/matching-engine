package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/orchidknight/matching-engine/models"
)

func TestMatchOneToOneCompletesMakerAndTaker(t *testing.T) {
	testBook := newTestOrderbook()
	makerOrder := newRestingOrder(models.Sell, decimal.NewFromInt(3))
	makerOrder.Price = decimal.NewFromInt(100)

	if err := testBook.InsertOrder(makerOrder); err != nil {
		t.Fatalf("InsertOrder() error = %v", err)
	}

	takerOrder := newLimitOrder(models.Buy, decimal.NewFromInt(3), decimal.NewFromInt(100))
	response, err := testBook.Match(context.Background(), takerOrder)
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}

	requireOrderState(t, response.InitialOrder, models.OrderStatusCompleted, decimal.Zero, decimal.NewFromInt(3), decimal.NewFromInt(300))
	requireMatchedOrderCount(t, response, 1)
	requireOrderState(t, response.MatchedOrders[0].Order, models.OrderStatusCompleted, decimal.Zero, decimal.NewFromInt(3), decimal.NewFromInt(300))
	requireTrade(t, response.MatchedOrders[0].Trade, takerOrder, makerOrder, decimal.NewFromInt(3), decimal.NewFromInt(100))
	requireLastPrice(t, response, decimal.NewFromInt(100))

	if _, exists := testBook.GetOrder(makerOrder.ID, "sell", makerOrder.Price); exists {
		t.Fatalf("maker order %s remains in asks after full fill", makerOrder.ID)
	}
}

func TestMatchOneToOnePartiallyFillsMaker(t *testing.T) {
	testBook := newTestOrderbook()
	makerOrder := newRestingOrder(models.Sell, decimal.NewFromInt(5))
	makerOrder.Price = decimal.NewFromInt(100)

	if err := testBook.InsertOrder(makerOrder); err != nil {
		t.Fatalf("InsertOrder() error = %v", err)
	}

	takerOrder := newLimitOrder(models.Buy, decimal.NewFromInt(2), decimal.NewFromInt(100))
	response, err := testBook.Match(context.Background(), takerOrder)
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}

	requireOrderState(t, response.InitialOrder, models.OrderStatusCompleted, decimal.Zero, decimal.NewFromInt(2), decimal.NewFromInt(200))
	requireMatchedOrderCount(t, response, 1)
	requireOrderState(t, response.MatchedOrders[0].Order, models.OrderStatusPartiallyCompleted, decimal.NewFromInt(3), decimal.NewFromInt(2), decimal.NewFromInt(200))
	requireTrade(t, response.MatchedOrders[0].Trade, takerOrder, makerOrder, decimal.NewFromInt(2), decimal.NewFromInt(100))

	remainingOrder, exists := testBook.GetOrder(makerOrder.ID, "sell", makerOrder.Price)
	if !exists {
		t.Fatalf("maker order %s was removed after partial fill", makerOrder.ID)
	}
	requireDecimalEqual(t, "remaining maker amount", remainingOrder.AvailableAmount, decimal.NewFromInt(3))
}

func TestMatchOneToManyCompletesMakersByPricePriority(t *testing.T) {
	testBook := newTestOrderbook()
	highPriceMaker := newRestingOrder(models.Sell, decimal.NewFromInt(3))
	highPriceMaker.Price = decimal.NewFromInt(101)
	lowPriceMaker := newRestingOrder(models.Sell, decimal.NewFromInt(2))
	lowPriceMaker.Price = decimal.NewFromInt(100)

	for _, makerOrder := range []*models.Order{highPriceMaker, lowPriceMaker} {
		if err := testBook.InsertOrder(makerOrder); err != nil {
			t.Fatalf("InsertOrder() error = %v", err)
		}
	}

	takerOrder := newLimitOrder(models.Buy, decimal.NewFromInt(5), decimal.NewFromInt(101))
	response, err := testBook.Match(context.Background(), takerOrder)
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}

	requireOrderState(t, response.InitialOrder, models.OrderStatusCompleted, decimal.Zero, decimal.NewFromInt(5), decimal.NewFromInt(503))
	requireMatchedOrderCount(t, response, 2)
	requireMatchedMaker(t, response.MatchedOrders[0], lowPriceMaker, models.OrderStatusCompleted, decimal.NewFromInt(2))
	requireMatchedMaker(t, response.MatchedOrders[1], highPriceMaker, models.OrderStatusCompleted, decimal.NewFromInt(3))
	requireLastPrice(t, response, decimal.NewFromInt(101))

	snapshot := testBook.Snapshot()
	if len(snapshot.Asks) != 0 {
		t.Fatalf("Snapshot().Asks length = %d, want 0", len(snapshot.Asks))
	}
}

func TestMatchAggressiveLimitPartiallyFillsAndRestsRemainder(t *testing.T) {
	testBook := newTestOrderbook()
	makerOrder := newRestingOrder(models.Sell, decimal.NewFromInt(2))
	makerOrder.Price = decimal.NewFromInt(100)

	if err := testBook.InsertOrder(makerOrder); err != nil {
		t.Fatalf("InsertOrder() error = %v", err)
	}

	takerOrder := newLimitOrder(models.Buy, decimal.NewFromInt(5), decimal.NewFromInt(105))
	response, err := testBook.Match(context.Background(), takerOrder)
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}

	requireOrderState(t, response.InitialOrder, models.OrderStatusPartiallyCompleted, decimal.NewFromInt(3), decimal.NewFromInt(2), decimal.NewFromInt(200))
	requireMatchedOrderCount(t, response, 1)
	requireMatchedMaker(t, response.MatchedOrders[0], makerOrder, models.OrderStatusCompleted, decimal.NewFromInt(2))
	requireLastPrice(t, response, decimal.NewFromInt(100))

	restingTaker, exists := testBook.GetOrder(takerOrder.ID, "buy", takerOrder.Price)
	if !exists {
		t.Fatalf("aggressive limit remainder %s was not inserted into bids", takerOrder.ID)
	}
	requireDecimalEqual(t, "resting taker amount", restingTaker.AvailableAmount, decimal.NewFromInt(3))
}

func TestCanMatchImmediatelyBoundaryPrices(t *testing.T) {
	tests := []struct {
		name       string
		makerSide  models.Side
		makerPrice decimal.Decimal
		takerSide  models.Side
		takerPrice decimal.Decimal
		want       bool
	}{
		{
			name:       "buy limit equals best ask",
			makerSide:  models.Sell,
			makerPrice: decimal.NewFromInt(100),
			takerSide:  models.Buy,
			takerPrice: decimal.NewFromInt(100),
			want:       true,
		},
		{
			name:       "buy limit below best ask",
			makerSide:  models.Sell,
			makerPrice: decimal.NewFromInt(100),
			takerSide:  models.Buy,
			takerPrice: decimal.NewFromInt(99),
			want:       false,
		},
		{
			name:       "sell limit equals best bid",
			makerSide:  models.Buy,
			makerPrice: decimal.NewFromInt(100),
			takerSide:  models.Sell,
			takerPrice: decimal.NewFromInt(100),
			want:       true,
		},
		{
			name:       "sell limit above best bid",
			makerSide:  models.Buy,
			makerPrice: decimal.NewFromInt(100),
			takerSide:  models.Sell,
			takerPrice: decimal.NewFromInt(101),
			want:       false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			testBook := newTestOrderbook()
			makerOrder := newRestingOrder(testCase.makerSide, decimal.NewFromInt(1))
			makerOrder.Price = testCase.makerPrice

			if err := testBook.InsertOrder(makerOrder); err != nil {
				t.Fatalf("InsertOrder() error = %v", err)
			}

			takerOrder := newLimitOrder(testCase.takerSide, decimal.NewFromInt(1), testCase.takerPrice)
			got := testBook.CanMatchImmediately(takerOrder)
			if got != testCase.want {
				t.Fatalf("CanMatchImmediately() = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestMatchOrderWithAmountBuildsPricePriorityResult(t *testing.T) {
	testBook := newTestOrderbook()
	highPriceMaker := newRestingOrder(models.Sell, decimal.NewFromInt(3))
	highPriceMaker.Price = decimal.NewFromInt(101)
	lowPriceMaker := newRestingOrder(models.Sell, decimal.NewFromInt(2))
	lowPriceMaker.Price = decimal.NewFromInt(100)

	for _, makerOrder := range []*models.Order{highPriceMaker, lowPriceMaker} {
		if err := testBook.InsertOrder(makerOrder); err != nil {
			t.Fatalf("InsertOrder() error = %v", err)
		}
	}

	takerOrder := newLimitOrder(models.Buy, decimal.NewFromInt(4), decimal.NewFromInt(101))
	result := testBook.MatchOrderWithAmount(takerOrder)

	if !result.IsDone {
		t.Fatal("MatchOrderWithAmount().IsDone = false, want true")
	}
	requireDecimalEqual(t, "amount left", result.AmountLeft, decimal.Zero)
	requireDecimalEqual(t, "amount done", result.AmountDone, decimal.NewFromInt(4))
	requireDecimalEqual(t, "total done", result.TotalDone, decimal.NewFromInt(402))
	requireRawMatchedOrder(t, result.MatchedOrders, 0, lowPriceMaker, true, decimal.NewFromInt(2))
	requireRawMatchedOrder(t, result.MatchedOrders, 1, highPriceMaker, false, decimal.NewFromInt(2))
}

func TestMatchOrderWithTotalBuildsPartialPriceLevelResult(t *testing.T) {
	testBook := newTestOrderbook()
	highPriceMaker := newRestingOrder(models.Sell, decimal.NewFromInt(5))
	highPriceMaker.Price = decimal.NewFromInt(120)
	lowPriceMaker := newRestingOrder(models.Sell, decimal.NewFromInt(2))
	lowPriceMaker.Price = decimal.NewFromInt(100)

	for _, makerOrder := range []*models.Order{highPriceMaker, lowPriceMaker} {
		if err := testBook.InsertOrder(makerOrder); err != nil {
			t.Fatalf("InsertOrder() error = %v", err)
		}
	}

	takerOrder := &models.Order{
		ID:             uuid.New(),
		Account:        "taker",
		Symbol:         "BTC-USDT",
		Type:           models.OrderTypeMarket,
		Status:         models.OrderStatusNew,
		Side:           models.Buy,
		AvailableTotal: decimal.NewFromInt(320),
	}

	result := testBook.MatchOrderWithTotal(takerOrder)

	if !result.IsDone {
		t.Fatal("MatchOrderWithTotal().IsDone = false, want true")
	}
	requireDecimalEqual(t, "total left", result.TotalLeft, decimal.Zero)
	requireDecimalEqual(t, "total done", result.TotalDone, decimal.NewFromInt(320))
	requireDecimalEqual(t, "amount done", result.AmountDone, decimal.NewFromInt(3))
	requireRawMatchedOrder(t, result.MatchedOrders, 0, lowPriceMaker, true, decimal.NewFromInt(2))
	requireRawMatchedOrder(t, result.MatchedOrders, 1, highPriceMaker, false, decimal.NewFromInt(1))
}

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

	response, err := testBook.Match(context.Background(), takerOrder)
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}

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

func TestMatchRollsBackRestingTakerWhenPersistFails(t *testing.T) {
	testBook := newTestOrderbook()
	testBook.orders = &failingOrderService{updateErr: errors.New("update order failed")}
	restingAsk := newRestingOrder(models.Sell, decimal.NewFromInt(5))
	restingAsk.Account = "account-1"

	if err := testBook.InsertOrder(restingAsk); err != nil {
		t.Fatalf("InsertOrder() error = %v", err)
	}

	takerOrder := newLimitOrder(models.Buy, decimal.NewFromInt(3), decimal.NewFromInt(100))
	takerOrder.Account = restingAsk.Account

	response, err := testBook.Match(context.Background(), takerOrder)
	if err == nil {
		t.Fatal("Match() error is nil, want non-nil")
	}
	if response != nil {
		t.Fatalf("Match() response = %+v, want nil", response)
	}
	if takerOrder.Status != models.OrderStatusNew {
		t.Fatalf("taker status = %s, want %s", takerOrder.Status, models.OrderStatusNew)
	}

	priceItem := testBook.bidsTree.Get(newPriceNode(takerOrder.Price))
	if priceItem != nil {
		if _, exists := priceItem.(*PriceNode).orderMap.Get(takerOrder.ID); exists {
			t.Fatalf("taker order %s remains in bids after persistence failure", takerOrder.ID)
		}
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

func requireOrderState(
	t *testing.T,
	order *models.Order,
	wantStatus models.OrderStatus,
	wantAvailableAmount decimal.Decimal,
	wantExecutedAmount decimal.Decimal,
	wantExecutedTotal decimal.Decimal,
) {
	t.Helper()

	if order == nil {
		t.Fatal("order is nil")
	}
	if order.Status != wantStatus {
		t.Fatalf("order.Status = %s, want %s", order.Status, wantStatus)
	}
	requireDecimalEqual(t, "order.AvailableAmount", order.AvailableAmount, wantAvailableAmount)
	requireDecimalEqual(t, "order.ExecutedAmount", order.ExecutedAmount, wantExecutedAmount)
	requireDecimalEqual(t, "order.ExecutedTotal", order.ExecutedTotal, wantExecutedTotal)
}

func requireMatchedOrderCount(t *testing.T, response *models.OrderResponse, want int) {
	t.Helper()

	if response == nil {
		t.Fatal("response is nil")
	}
	if len(response.MatchedOrders) != want {
		t.Fatalf("len(response.MatchedOrders) = %d, want %d", len(response.MatchedOrders), want)
	}
}

func requireMatchedMaker(
	t *testing.T,
	matchedOrder *models.MatchedOrderResult,
	wantMaker *models.Order,
	wantStatus models.OrderStatus,
	wantMatchedAmount decimal.Decimal,
) {
	t.Helper()

	if matchedOrder == nil {
		t.Fatal("matched order is nil")
	}
	if matchedOrder.Order.ID != wantMaker.ID {
		t.Fatalf("matched maker ID = %s, want %s", matchedOrder.Order.ID, wantMaker.ID)
	}
	requireOrderState(t, matchedOrder.Order, wantStatus, wantMaker.AvailableAmount.Sub(wantMatchedAmount), wantMatchedAmount, wantMatchedAmount.Mul(wantMaker.Price))
}

func requireRawMatchedOrder(
	t *testing.T,
	matchedOrders []*models.MatchedOrder,
	index int,
	wantMaker *models.Order,
	wantDone bool,
	wantMatchedAmount decimal.Decimal,
) {
	t.Helper()

	if len(matchedOrders) <= index {
		t.Fatalf("len(matchedOrders) = %d, want index %d", len(matchedOrders), index)
	}

	matchedOrder := matchedOrders[index]
	if matchedOrder.Order.ID != wantMaker.ID {
		t.Fatalf("matchedOrders[%d].Order.ID = %s, want %s", index, matchedOrder.Order.ID, wantMaker.ID)
	}
	if matchedOrder.IsDone != wantDone {
		t.Fatalf("matchedOrders[%d].IsDone = %v, want %v", index, matchedOrder.IsDone, wantDone)
	}
	requireDecimalEqual(t, "matched amount", matchedOrder.MatchedAmount, wantMatchedAmount)
}

func requireTrade(
	t *testing.T,
	trade *models.Trade,
	takerOrder *models.Order,
	makerOrder *models.Order,
	wantAmount decimal.Decimal,
	wantPrice decimal.Decimal,
) {
	t.Helper()

	if trade == nil {
		t.Fatal("trade is nil")
	}
	if trade.TakerOrderID != takerOrder.ID {
		t.Fatalf("trade.TakerOrderID = %s, want %s", trade.TakerOrderID, takerOrder.ID)
	}
	if trade.MakerOrderID != makerOrder.ID {
		t.Fatalf("trade.MakerOrderID = %s, want %s", trade.MakerOrderID, makerOrder.ID)
	}
	if trade.TakerSide != takerOrder.Side {
		t.Fatalf("trade.TakerSide = %s, want %s", trade.TakerSide, takerOrder.Side)
	}
	requireDecimalEqual(t, "trade.Amount", trade.Amount, wantAmount)
	requireDecimalEqual(t, "trade.Price", trade.Price, wantPrice)
}

func requireLastPrice(t *testing.T, response *models.OrderResponse, want decimal.Decimal) {
	t.Helper()

	if response.LastPrice == nil {
		t.Fatal("response.LastPrice is nil")
	}
	requireDecimalEqual(t, "response.LastPrice", *response.LastPrice, want)
}

func requireDecimalEqual(t *testing.T, name string, got decimal.Decimal, want decimal.Decimal) {
	t.Helper()

	if !got.Equal(want) {
		t.Fatalf("%s = %s, want %s", name, got, want)
	}
}

type failingOrderService struct {
	updateErr error
	rejectErr error
}

func (service *failingOrderService) UpdateOrder(context.Context, *models.Order) error {
	return service.updateErr
}

func (*failingOrderService) GetOrderByID(context.Context, uuid.UUID) (*models.Order, error) {
	return nil, nil
}

func (*failingOrderService) GetOrdersByPair(context.Context, string) ([]*models.Order, error) {
	return nil, nil
}

func (service *failingOrderService) Reject(context.Context, *models.Order) error {
	return service.rejectErr
}
