package engine

import (
	"context"
	"fmt"
	"log"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/orchidknight/matching-engine/models"
)

func TestEngine_ConsumeOrder(t *testing.T) {
	tests := []struct {
		name              string
		inputOrder        *models.Order
		wantOrderResponse *models.OrderResponse
		wantErr           any
	}{
		{
			name: "wrong symbol",
			inputOrder: &models.Order{
				ID:             uuid.New(),
				Account:        "user",
				Symbol:         "BTC-USDC",
				Type:           models.OrderTypeMarket,
				Status:         models.OrderStatusNew,
				Side:           models.Buy,
				AvailableTotal: decimal.NewFromUint64(1000),
			},
			wantOrderResponse: &models.OrderResponse{
				Symbol: "BTC-USDC",
				InitialOrder: &models.Order{
					Account:        "user",
					Symbol:         "BTC-USDC",
					Type:           models.OrderTypeMarket,
					Status:         models.OrderStatusRejected,
					RejectedReason: models.RejectReasonWrongSymbol,
					Side:           models.Buy,
					AvailableTotal: decimal.NewFromUint64(1000),
				},
			},
		},
		{
			name: "added to orderbook",
			inputOrder: &models.Order{
				ID:              uuid.New(),
				Account:         "user",
				Symbol:          "BTC-USDT",
				Type:            models.OrderTypeLimit,
				Status:          models.OrderStatusNew,
				Side:            models.Buy,
				AvailableAmount: decimal.NewFromUint64(1),
				Price:           decimal.NewFromUint64(100000),
			},
			wantOrderResponse: &models.OrderResponse{
				Symbol: "BTC-USDT",
				InitialOrder: &models.Order{
					Account:         "user",
					Symbol:          "BTC-USDT",
					Type:            models.OrderTypeLimit,
					Status:          models.OrderStatusOpen,
					Side:            models.Buy,
					AvailableAmount: decimal.NewFromUint64(1),
					Price:           decimal.NewFromUint64(100000),
				},
			},
		},
		{
			name: "added to stop listener",
			inputOrder: &models.Order{
				ID:              uuid.New(),
				Account:         "user",
				Symbol:          "BTC-USDT",
				Type:            models.OrderTypeStopLimit,
				Status:          models.OrderStatusNew,
				Side:            models.Buy,
				AvailableAmount: decimal.NewFromUint64(1),
				Price:           decimal.NewFromUint64(100000),
				ActivationPrice: decimal.NewFromUint64(90000),
			},
			wantOrderResponse: &models.OrderResponse{
				Symbol: "BTC-USDT",
				InitialOrder: &models.Order{
					Account:         "user",
					Symbol:          "BTC-USDT",
					Type:            models.OrderTypeStopLimit,
					Status:          models.OrderStatusPendingTriggerPrice,
					Side:            models.Buy,
					AvailableAmount: decimal.NewFromUint64(1),
					Price:           decimal.NewFromUint64(100000),
					ActivationPrice: decimal.NewFromUint64(90000),
					ActivationType:  models.ActivationTypeMore,
				},
			},
		},
		{
			name: "not enough liquidity",
			inputOrder: &models.Order{
				ID:             uuid.New(),
				Account:        "user",
				Symbol:         "BTC-USDT",
				Type:           models.OrderTypeMarket,
				Status:         models.OrderStatusNew,
				Side:           models.Buy,
				AvailableTotal: decimal.NewFromUint64(100000),
			},
			wantOrderResponse: &models.OrderResponse{
				Symbol: "BTC-USDT",
				InitialOrder: &models.Order{
					ID:             uuid.New(),
					Account:        "user",
					Symbol:         "BTC-USDT",
					Type:           models.OrderTypeMarket,
					Status:         models.OrderStatusRejected,
					RejectedReason: models.RejectReasonNoMatches,
					Side:           models.Buy,
					AvailableTotal: decimal.NewFromUint64(100000),
				},
			},
		},
		//"matched 1 to 1 order, maker order completed":           {},
		//"matched 1 to 1 order, maker order partially completed": {},
		//"matched 1 to 2 orders, maker orders completed":         {},
		//"matched aggressive limit order":                        {},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			engine := newRunningTestEngine(t, NewOrdersMock())
			engine.ConsumeOrder(testCase.inputOrder)

			actualOrderResponse := readOrderResponse(t, engine)
			err := compareOrderResponse(actualOrderResponse, testCase.wantOrderResponse)
			if err != nil {
				t.Errorf("order responses do not match: %v;  actual: %v want: %v", err, actualOrderResponse, testCase.wantOrderResponse)
			}
		})
	}
}

func newRunningTestEngine(t *testing.T, orders models.OrderService) *Engine {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	engine, err := NewEngine(ctx, NewMarketsMock(), orders, NewTradesMock(), NewOrderbookMock(), NewLogMock())
	if err != nil {
		cancel()
		t.Fatal("can't initialize engine", err.Error())
	}

	runErr := make(chan error, 1)
	go func() {
		runErr <- engine.Run(ctx)
	}()

	t.Cleanup(func() {
		cancel()

		select {
		case err := <-runErr:
			if err != nil {
				t.Errorf("engine.Run() error = %v", err)
			}
		case <-time.After(time.Second):
			t.Error("engine.Run() did not stop after context cancellation")
		}
	})

	return engine
}

func TestEngineRunStopsWhenResponseChannelIsFullAndContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	orders := &notifyingOrderService{
		updated: make(chan struct{}, 1),
	}
	engine, err := NewEngine(ctx, NewMarketsMock(), orders, NewTradesMock(), NewOrderbookMock(), NewLogMock())
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	for len(engine.outcomingOrderResponses) < cap(engine.outcomingOrderResponses) {
		engine.outcomingOrderResponses <- &models.OrderResponse{}
	}

	runErr := make(chan error, 1)
	go func() {
		runErr <- engine.Run(ctx)
	}()

	engine.ConsumeOrder(&models.Order{
		ID:              uuid.New(),
		Account:         "user",
		Symbol:          "BTC-USDT",
		Type:            models.OrderTypeLimit,
		Status:          models.OrderStatusNew,
		Side:            models.Buy,
		AvailableAmount: decimal.NewFromInt(1),
		Price:           decimal.NewFromInt(100000),
	})

	select {
	case <-orders.updated:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for order update before blocked response send")
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

type LogMock struct{}

func NewLogMock() models.Logger {
	return &LogMock{}
}

func (*LogMock) Debug(component string, format string, a ...any) {
	log.Printf(fmt.Sprintf("%-6s | %s", component, format), a...)
}

func (*LogMock) Info(component string, format string, a ...any) {
	log.Printf(fmt.Sprintf("%-6s | %s", component, format), a...)
}

func (*LogMock) Warn(component string, format string, a ...any) {
	log.Printf(fmt.Sprintf("%-6s | %s", component, format), a...)
}

func (*LogMock) Error(component string, format string, a ...any) {
	log.Printf(fmt.Sprintf("%-6s | %s", component, format), a...)
}

func (*LogMock) Fatal(component string, format string, a ...any) {
	log.Printf(fmt.Sprintf("| %-6s |%s", component, format), a...)
}

type MarketServiceMock struct {
}

func NewMarketsMock() models.MarketService {
	return &MarketServiceMock{}
}

var MarketBTCUSDT = &models.Market{
	ID:            "BTC-USDT",
	BaseAsset:     &models.Asset{ID: "BTC", Name: "BTC"},
	QuoteAsset:    &models.Asset{ID: "USDT", Name: "USDT"},
	MinOrderSize:  decimal.NewFromInt(1),
	IsPublished:   true,
	LastSpotPrice: decimal.Zero,
}

func (*MarketServiceMock) GetMarkets() ([]*models.Market, error) {
	return []*models.Market{MarketBTCUSDT}, nil
}

func (*MarketServiceMock) UpdateMarket(*models.Market) error {
	return nil
}

func (*MarketServiceMock) GetMarketByID(string) (*models.Market, error) {
	return MarketBTCUSDT, nil
}

func NewOrdersMock() models.OrderService {
	return &OrderServiceMock{}
}

type OrderServiceMock struct {
}

func (*OrderServiceMock) UpdateOrder(context.Context, *models.Order) error {
	return nil
}

func (*OrderServiceMock) GetOrderByID(context.Context, uuid.UUID) (*models.Order, error) {
	return nil, nil
}

func (*OrderServiceMock) GetOrdersByPair(context.Context, string) ([]*models.Order, error) {
	return nil, nil
}

func (*OrderServiceMock) Reject(context.Context, *models.Order) error {
	return nil
}

type notifyingOrderService struct {
	OrderServiceMock
	updated chan struct{}
}

func (service *notifyingOrderService) UpdateOrder(context.Context, *models.Order) error {
	select {
	case service.updated <- struct{}{}:
	default:
	}

	return nil
}

func NewTradesMock() models.TradeService {
	return &TradeServiceMock{}
}

type TradeServiceMock struct {
}

func (*TradeServiceMock) LastPrice(models.Symbol) decimal.Decimal {
	return decimal.Decimal{}
}

func (*TradeServiceMock) ConsumeTrade(context.Context, *models.Trade) error {
	return nil
}

func NewOrderbookMock() models.OrderbookService {
	return &OrderbookServiceMock{}
}

type OrderbookServiceMock struct {
}

func (*OrderbookServiceMock) ConsumeTrade(context.Context, *models.Trade) error {
	return nil
}

// compareOrderResponse compares two *OrderResponse for deep equivalence.
// It returns a formatted error that precisely points to the first differing field.
// If everything is equivalent, it returns nil.
func compareOrderResponse(a, b *models.OrderResponse) error {
	path := "OrderResponse"

	if a == nil && b == nil {
		return nil
	}
	if a == nil || b == nil {
		return diff(path, a, b)
	}

	if a.Symbol != b.Symbol {
		return diff(path+".Symbol", a.Symbol, b.Symbol)
	}

	if err := cmpOrder(path+".InitialOrder", a.InitialOrder, b.InitialOrder); err != nil {
		return err
	}

	// MatchedOrders slice
	if len(a.MatchedOrders) != len(b.MatchedOrders) {
		return diff(path+".MatchedOrders.length", len(a.MatchedOrders), len(b.MatchedOrders))
	}
	for i := range a.MatchedOrders {
		p := fmt.Sprintf("%s.MatchedOrders[%d]", path, i)
		if err := cmpMatchedOrderResult(p, a.MatchedOrders[i], b.MatchedOrders[i]); err != nil {
			return err
		}
	}

	// LastPrice pointer
	return cmpDecimalPtr(path+".LastPrice", a.LastPrice, b.LastPrice)
}

func cmpMatchedOrderResult(path string, a, b *models.MatchedOrderResult) error {
	if a == nil && b == nil {
		return nil
	}
	if a == nil || b == nil {
		return diff(path, a, b)
	}

	if err := cmpOrder(path+".Order", a.Order, b.Order); err != nil {
		return err
	}

	return cmpTrade(path+".Trade", a.Trade, b.Trade)
}

//nolint:revive // cmpOrder intentionally reports the first differing field explicitly.
func cmpOrder(path string, a, b *models.Order) error {
	if a == nil && b == nil {
		return nil
	}
	if a == nil || b == nil {
		return diff(path, a, b)
	}

	if a.Account != b.Account {
		return diff(path+".Account", a.Account, b.Account)
	}

	// enums / aliases
	if a.Symbol != b.Symbol {
		return diff(path+".Symbol", a.Symbol, b.Symbol)
	}
	if a.Type != b.Type {
		return diff(path+".Type", a.Type, b.Type)
	}
	if a.Side != b.Side {
		return diff(path+".Side", a.Side, b.Side)
	}
	if a.Status != b.Status {
		return diff(path+".Status", a.Status, b.Status)
	}
	if a.RejectedReason != b.RejectedReason {
		return diff(path+".RejectedReason", a.RejectedReason, b.RejectedReason)
	}

	// decimals
	if err := cmpDecimal(path+".AvailableAmount", a.AvailableAmount, b.AvailableAmount); err != nil {
		return err
	}
	if err := cmpDecimal(path+".ExecutedAmount", a.ExecutedAmount, b.ExecutedAmount); err != nil {
		return err
	}
	if err := cmpDecimal(path+".CanceledAmount", a.CanceledAmount, b.CanceledAmount); err != nil {
		return err
	}

	if err := cmpDecimal(path+".AvailableTotal", a.AvailableTotal, b.AvailableTotal); err != nil {
		return err
	}
	if err := cmpDecimal(path+".ExecutedTotal", a.ExecutedTotal, b.ExecutedTotal); err != nil {
		return err
	}
	if err := cmpDecimal(path+".CanceledTotal", a.CanceledTotal, b.CanceledTotal); err != nil {
		return err
	}

	if err := cmpDecimal(path+".Price", a.Price, b.Price); err != nil {
		return err
	}
	if err := cmpDecimal(path+".ActivationPrice", a.ActivationPrice, b.ActivationPrice); err != nil {
		return err
	}
	if a.ActivationType != b.ActivationType {
		return diff(path+".ActivationType", a.ActivationType, b.ActivationType)
	}
	if err := cmpDecimal(path+".AvgPrice", a.AvgPrice, b.AvgPrice); err != nil {
		return err
	}

	// LastTrade pointer
	if err := cmpTrade(path+".LastTrade", a.LastTrade, b.LastTrade); err != nil {
		return err
	}

	// time
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return diff(path+".CreatedAt", a.CreatedAt.Format(time.RFC3339Nano), b.CreatedAt.Format(time.RFC3339Nano))
	}

	return nil
}

//nolint:revive // cmpTrade intentionally reports the first differing field explicitly.
func cmpTrade(path string, a, b *models.Trade) error {
	if a == nil && b == nil {
		return nil
	}
	if a == nil || b == nil {
		return diff(path, a, b)
	}

	if a.ID != b.ID {
		return diff(path+".ID", a.ID, b.ID)
	}

	// UUIDs
	if a.TakerOrderID != b.TakerOrderID {
		return diff(path+".TakerOrderID", a.TakerOrderID.String(), b.TakerOrderID.String())
	}
	if a.MakerOrderID != b.MakerOrderID {
		return diff(path+".MakerOrderID", a.MakerOrderID.String(), b.MakerOrderID.String())
	}

	// strings
	if a.TakerID != b.TakerID {
		return diff(path+".TakerID", a.TakerID, b.TakerID)
	}
	if a.MakerID != b.MakerID {
		return diff(path+".MakerID", a.MakerID, b.MakerID)
	}

	// enums / aliases
	if a.Symbol != b.Symbol {
		return diff(path+".Symbol", a.Symbol, b.Symbol)
	}
	if a.TakerSide != b.TakerSide {
		return diff(path+".TakerSide", a.TakerSide, b.TakerSide)
	}
	if a.Status != b.Status {
		return diff(path+".Status", a.Status, b.Status)
	}

	// decimals
	if err := cmpDecimal(path+".Amount", a.Amount, b.Amount); err != nil {
		return err
	}
	if err := cmpDecimal(path+".Price", a.Price, b.Price); err != nil {
		return err
	}

	// time
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return diff(path+".CreatedAt", a.CreatedAt.Format(time.RFC3339Nano), b.CreatedAt.Format(time.RFC3339Nano))
	}

	return nil
}

func cmpDecimal(path string, a, b decimal.Decimal) error {
	if !a.Equal(b) {
		return diff(path, a.String(), b.String())
	}

	return nil
}

func cmpDecimalPtr(path string, a, b *decimal.Decimal) error {
	if a == nil && b == nil {
		return nil
	}
	if a == nil || b == nil {
		var av, bv any
		if a != nil {
			av = a.String()
		}
		if b != nil {
			bv = b.String()
		}

		return diff(path, av, bv)
	}
	if !a.Equal(*b) {
		return diff(path, a.String(), b.String())
	}

	return nil
}

func diff(path string, a, b any) error {
	return fmt.Errorf("mismatch at %s: %v != %v", path, a, b)
}
