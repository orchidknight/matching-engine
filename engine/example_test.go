package engine_test

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/orchidknight/matching-engine/engine"
	"github.com/orchidknight/matching-engine/models"
)

func Example() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	symbol := models.Symbol("BTC-USDT")
	makerOrder := &models.Order{
		ID:              uuid.New(),
		Account:         "maker",
		Symbol:          symbol,
		Type:            models.OrderTypeLimit,
		Status:          models.OrderStatusOpen,
		Side:            models.Sell,
		AvailableAmount: decimal.NewFromInt(1),
		Price:           decimal.NewFromInt(100),
	}

	orderService := newExampleOrderService(makerOrder)
	matchingEngine, err := engine.NewEngine(
		ctx,
		&exampleMarketService{market: exampleMarket(symbol)},
		orderService,
		&exampleTradeService{},
		&exampleOrderbookService{},
		&exampleLogger{},
	)
	if err != nil {
		panic(err)
	}

	runErr := make(chan error, 1)
	go func() {
		runErr <- matchingEngine.Run(ctx)
	}()

	takerOrder := &models.Order{
		ID:              uuid.New(),
		Account:         "taker",
		Symbol:          symbol,
		Type:            models.OrderTypeLimit,
		Status:          models.OrderStatusNew,
		Side:            models.Buy,
		AvailableAmount: decimal.NewFromInt(1),
		Price:           decimal.NewFromInt(100),
	}

	matchingEngine.ConsumeOrder(takerOrder)
	response := matchingEngine.GetLastOrderResponse()

	lastPrice := "none"
	if response.LastPrice != nil {
		lastPrice = response.LastPrice.String()
	}

	fmt.Printf("taker status: %s\n", response.InitialOrder.Status)
	fmt.Printf("matched orders: %d at %s\n", len(response.MatchedOrders), lastPrice)
	fmt.Printf("maker status: %s\n", response.MatchedOrders[0].Order.Status)

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			panic(err)
		}
	case <-time.After(time.Second):
		panic("engine did not stop")
	}

	// Output:
	// taker status: Completed
	// matched orders: 1 at 100
	// maker status: Completed
}

func exampleMarket(symbol models.Symbol) *models.Market {
	return &models.Market{
		ID: symbol,
		BaseAsset: &models.Asset{
			ID:                   "BTC",
			Name:                 "Bitcoin",
			InputPrecision:       8,
			CalculationPrecision: 8,
		},
		QuoteAsset: &models.Asset{
			ID:                   "USDT",
			Name:                 "Tether USD",
			InputPrecision:       2,
			CalculationPrecision: 2,
		},
		MinOrderSize: decimal.NewFromInt(1),
		IsPublished:  true,
	}
}

type exampleMarketService struct {
	market *models.Market
}

func (service *exampleMarketService) GetMarkets() ([]*models.Market, error) {
	return []*models.Market{service.market}, nil
}

func (service *exampleMarketService) UpdateMarket(market *models.Market) error {
	service.market = market

	return nil
}

func (service *exampleMarketService) GetMarketByID(string) (*models.Market, error) {
	return service.market, nil
}

type exampleOrderService struct {
	lock   sync.Mutex
	orders map[uuid.UUID]*models.Order
}

func newExampleOrderService(initialOrders ...*models.Order) *exampleOrderService {
	service := &exampleOrderService{
		orders: make(map[uuid.UUID]*models.Order, len(initialOrders)),
	}
	for _, order := range initialOrders {
		service.orders[order.ID] = order
	}

	return service
}

func (service *exampleOrderService) UpdateOrder(_ context.Context, order *models.Order) error {
	service.lock.Lock()
	defer service.lock.Unlock()

	service.orders[order.ID] = order

	return nil
}

func (service *exampleOrderService) GetOrderByID(_ context.Context, id uuid.UUID) (*models.Order, error) {
	service.lock.Lock()
	defer service.lock.Unlock()

	return service.orders[id], nil
}

func (service *exampleOrderService) GetOrdersByPair(_ context.Context, pair string) ([]*models.Order, error) {
	service.lock.Lock()
	defer service.lock.Unlock()

	orders := make([]*models.Order, 0, len(service.orders))
	for _, order := range service.orders {
		if order.Symbol.String() == pair {
			orders = append(orders, order)
		}
	}

	return orders, nil
}

func (service *exampleOrderService) Reject(ctx context.Context, order *models.Order) error {
	return service.UpdateOrder(ctx, order)
}

type exampleTradeService struct{}

func (*exampleTradeService) ConsumeTrade(context.Context, *models.Trade) error {
	return nil
}

func (*exampleTradeService) LastPrice(models.Symbol) decimal.Decimal {
	return decimal.Zero
}

type exampleOrderbookService struct{}

func (*exampleOrderbookService) ConsumeTrade(context.Context, *models.Trade) error {
	return nil
}

type exampleLogger struct{}

func (*exampleLogger) Debug(string, string, ...any) {}
func (*exampleLogger) Info(string, string, ...any)  {}
func (*exampleLogger) Warn(string, string, ...any)  {}
func (*exampleLogger) Error(string, string, ...any) {}
func (*exampleLogger) Fatal(string, string, ...any) {}
