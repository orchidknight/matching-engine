package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/orchidknight/matching-engine/models"
)

func TestEnginePushPriceTriggersStopLimitOrder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	orders := newRecordingOrderService()
	trades := &staticTradeService{lastPrice: decimal.NewFromInt(100)}

	engine, err := NewEngine(ctx, NewMarketsMock(), orders, trades, NewOrderbookMock(), NewLogMock())
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	runErr := make(chan error, 1)
	go func() {
		runErr <- engine.Run(ctx)
	}()

	stopOrder := &models.Order{
		ID:              uuid.New(),
		Account:         "user",
		Symbol:          "BTC-USDT",
		Type:            models.OrderTypeStopLimit,
		Status:          models.OrderStatusNew,
		Side:            models.Buy,
		AvailableAmount: decimal.NewFromInt(1),
		Price:           decimal.NewFromInt(80),
		ActivationPrice: decimal.NewFromInt(90),
	}

	engine.ConsumeOrder(stopOrder)

	pendingResponse := readOrderResponse(t, engine)
	if pendingResponse.InitialOrder.Status != models.OrderStatusPendingTriggerPrice {
		t.Fatalf("initial stop order status = %s, want %s",
			pendingResponse.InitialOrder.Status, models.OrderStatusPendingTriggerPrice)
	}
	if pendingResponse.InitialOrder.ActivationType != models.ActivationTypeLess {
		t.Fatalf("activation type = %s, want %s",
			pendingResponse.InitialOrder.ActivationType, models.ActivationTypeLess)
	}

	if !engine.PushPrice(models.Price{Symbol: "BTC-USDT", LastPrice: decimal.NewFromInt(90)}) {
		t.Fatal("PushPrice() = false, want true")
	}

	triggeredResponse := readOrderResponse(t, engine)
	if triggeredResponse.InitialOrder.ID != stopOrder.ID {
		t.Fatalf("triggered order ID = %s, want %s", triggeredResponse.InitialOrder.ID, stopOrder.ID)
	}
	if triggeredResponse.InitialOrder.Status != models.OrderStatusOpen {
		t.Fatalf("triggered order status = %s, want %s",
			triggeredResponse.InitialOrder.Status, models.OrderStatusOpen)
	}

	assertStatusSequence(t, orders.statuses(stopOrder.ID), []models.OrderStatus{
		models.OrderStatusPendingTriggerPrice,
		models.OrderStatusTriggered,
		models.OrderStatusOpen,
	})

	marketHandler, ok := engine.getMarketHandler("BTC-USDT")
	if !ok {
		t.Fatal("market handler BTC-USDT not found")
	}
	stopListener := marketHandler.stopListener.(*StopOrderListener)
	remainingOrders, err := stopListener.LessOrders(decimal.NewFromInt(90))
	if err != nil {
		t.Fatalf("LessOrders() error = %v", err)
	}
	if len(remainingOrders) != 0 {
		t.Fatalf("remaining triggered stop orders = %d, want 0", len(remainingOrders))
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Engine.Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Engine.Run() did not stop after context cancellation")
	}
}

func readOrderResponse(t *testing.T, engine *Engine) *models.OrderResponse {
	t.Helper()

	select {
	case response := <-engine.outcomingOrderResponses:
		return response
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for order response")
	}

	return nil
}

func assertStatusSequence(t *testing.T, got []models.OrderStatus, want []models.OrderStatus) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("status updates = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("status update %d = %s, want %s; all statuses: %v", i, got[i], want[i], got)
		}
	}
}

type recordingOrderService struct {
	lock            sync.Mutex
	orders          map[uuid.UUID]*models.Order
	statusesByOrder map[uuid.UUID][]models.OrderStatus
}

func newRecordingOrderService() *recordingOrderService {
	return &recordingOrderService{
		orders:          make(map[uuid.UUID]*models.Order),
		statusesByOrder: make(map[uuid.UUID][]models.OrderStatus),
	}
}

func (service *recordingOrderService) UpdateOrder(_ context.Context, order *models.Order) error {
	service.lock.Lock()
	defer service.lock.Unlock()

	orderCopy := *order
	service.orders[order.ID] = &orderCopy
	service.statusesByOrder[order.ID] = append(service.statusesByOrder[order.ID], order.Status)

	return nil
}

func (service *recordingOrderService) GetOrderByID(_ context.Context, id uuid.UUID) (*models.Order, error) {
	service.lock.Lock()
	defer service.lock.Unlock()

	order, ok := service.orders[id]
	if !ok {
		return nil, nil
	}

	orderCopy := *order

	return &orderCopy, nil
}

func (*recordingOrderService) GetOrdersByPair(_ context.Context, _ string) ([]*models.Order, error) {
	return nil, nil
}

func (service *recordingOrderService) Reject(_ context.Context, order *models.Order) error {
	service.lock.Lock()
	defer service.lock.Unlock()

	orderCopy := *order
	service.orders[order.ID] = &orderCopy
	service.statusesByOrder[order.ID] = append(service.statusesByOrder[order.ID], order.Status)

	return nil
}

func (service *recordingOrderService) statuses(id uuid.UUID) []models.OrderStatus {
	service.lock.Lock()
	defer service.lock.Unlock()

	return append([]models.OrderStatus(nil), service.statusesByOrder[id]...)
}

type staticTradeService struct {
	lastPrice decimal.Decimal
}

func (service *staticTradeService) LastPrice(models.Symbol) decimal.Decimal {
	return service.lastPrice
}

func (*staticTradeService) ConsumeTrade(context.Context, *models.Trade) error {
	return nil
}
