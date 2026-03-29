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
	tests := map[string]struct {
		inputOrder        *models.Order
		wantOrderResponse *models.OrderResponse
		wantErr           any
	}{
		"wrong symbol": {
			inputOrder: &models.Order{
				ID:             uuid.New(),
				Account:        "user",
				Symbol:         "BTC-USDC",
				Type:           models.OrderTypeMarket,
				Status:         models.OrderStatusNew,
				Side:           models.Buy,
				Total:          decimal.NewFromUint64(1000),
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
					Total:          decimal.NewFromUint64(1000),
					AvailableTotal: decimal.NewFromUint64(1000),
				},
			},
		},
		"added to orderbook": {
			inputOrder: &models.Order{
				ID:              uuid.New(),
				Account:         "user",
				Symbol:          "BTC-USDT",
				Type:            models.OrderTypeLimit,
				Status:          models.OrderStatusNew,
				Side:            models.Buy,
				Amount:          decimal.NewFromUint64(1),
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
					Amount:          decimal.NewFromUint64(1),
					AvailableAmount: decimal.NewFromUint64(1),
					Price:           decimal.NewFromUint64(100000),
				},
			},
		},
		"added to stop listener": {
			inputOrder: &models.Order{
				ID:              uuid.New(),
				Account:         "user",
				Symbol:          "BTC-USDT",
				Type:            models.OrderTypeStopLimit,
				Status:          models.OrderStatusNew,
				Side:            models.Buy,
				Amount:          decimal.NewFromUint64(1),
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
					Amount:          decimal.NewFromUint64(1),
					AvailableAmount: decimal.NewFromUint64(1),
					Price:           decimal.NewFromUint64(100000),
					ActivationPrice: decimal.NewFromUint64(90000),
					ActivationType:  models.ActivationTypeMore,
				},
			},
		},
		"not enough liquidity": {
			inputOrder: &models.Order{
				ID:             uuid.New(),
				Account:        "user",
				Symbol:         "BTC-USDT",
				Type:           models.OrderTypeMarket,
				Status:         models.OrderStatusNew,
				Side:           models.Buy,
				Total:          decimal.NewFromUint64(100000),
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
					Total:          decimal.NewFromUint64(100000),
					AvailableTotal: decimal.NewFromUint64(100000),
				},
			},
		},
		//"matched 1 to 1 order, maker order completed":           {},
		//"matched 1 to 1 order, maker order partially completed": {},
		//"matched 1 to 2 orders, maker orders completed":         {},
		//"matched aggressive limit order":                        {},
	}

	ctx := context.Background()
	logger := NewLogMock()
	markets := NewMarketsMock()
	orders := NewOrdersMock()
	trades := NewTradesMock()
	orderbook := NewOrderbookMock()

	engine, err := NewEngine(ctx, markets, orders, trades, orderbook, logger)
	if err != nil {
		t.Fatal("can't initialize engine", err.Error())
	}

	go engine.Run(ctx)

	time.Sleep(2 * time.Second)

	for name, tc := range tests {

		t.Run(name, func(t *testing.T) {
			engine.ConsumeOrder(tc.inputOrder)

			actualOrderResponse := engine.GetLastOrderResponse()
			fmt.Println("actual result: ", actualOrderResponse)
			err := compareOrderResponse(actualOrderResponse, tc.wantOrderResponse)
			if err != nil {
				t.Errorf("order responses do not match: %v;  actual: %v want: %v", err, actualOrderResponse, tc.wantOrderResponse)
			}

		})
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

func (msm *MarketServiceMock) GetMarkets() ([]*models.Market, error) {
	return []*models.Market{MarketBTCUSDT}, nil

}
func (msm *MarketServiceMock) UpdateMarket(market *models.Market) error {
	return nil
}

func (msm *MarketServiceMock) GetMarketByID(id string) (*models.Market, error) {
	return MarketBTCUSDT, nil
}

func NewOrdersMock() models.OrderService {
	return &OrderServiceMock{}
}

type OrderServiceMock struct {
}

func (osm *OrderServiceMock) UpdateOrder(ctx context.Context, order *models.Order) error {
	return nil
}
func (osm *OrderServiceMock) GetOrderByID(ctx context.Context, id uuid.UUID) (*models.Order, error) {
	return nil, nil
}
func (osm *OrderServiceMock) GetOrdersByPair(ctx context.Context, pair string) ([]*models.Order, error) {
	return nil, nil
}

func (osm *OrderServiceMock) Reject(ctx context.Context, o *models.Order) error {
	return nil

}

func NewTradesMock() models.TradeService {
	return &TradeServiceMock{}
}

type TradeServiceMock struct {
}

func (tsm *TradeServiceMock) LastPrice(s models.Symbol) decimal.Decimal {
	return decimal.Decimal{}

}

func (tsm *TradeServiceMock) ConsumeTrade(ctx context.Context, trade *models.Trade) error {
	return nil

}

func NewOrderbookMock() models.OrderbookService {
	return &OrderbookServiceMock{}
}

type OrderbookServiceMock struct {
}

func (osm *OrderbookServiceMock) ConsumeTrade(ctx context.Context, trade *models.Trade) error {
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
	if err := cmpDecimalPtr(path+".LastPrice", a.LastPrice, b.LastPrice); err != nil {
		return err
	}

	return nil
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
	if err := cmpTrade(path+".Trade", a.Trade, b.Trade); err != nil {
		return err
	}

	return nil
}

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
	if err := cmpDecimal(path+".Amount", a.Amount, b.Amount); err != nil {
		return err
	}
	if err := cmpDecimal(path+".AvailableAmount", a.AvailableAmount, b.AvailableAmount); err != nil {
		return err
	}
	if err := cmpDecimal(path+".ExecutedAmount", a.ExecutedAmount, b.ExecutedAmount); err != nil {
		return err
	}
	if err := cmpDecimal(path+".CanceledAmount", a.CanceledAmount, b.CanceledAmount); err != nil {
		return err
	}

	if err := cmpDecimal(path+".Total", a.Total, b.Total); err != nil {
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
