package engine

import (
	"context"
	"fmt"
	"sync"

	"github.com/orchidknight/matching-engine/models"
)

type Engine struct {
	MarketHandlerMap map[models.Symbol]*MarketHandler
	markets          models.MarketService
	orders           models.OrderService
	trades           models.TradeService
	orderbook        models.OrderbookService

	lock   sync.Mutex
	logger models.Logger
}

func NewEngine(
	ctx context.Context,
	markets models.MarketService,
	orders models.OrderService,
	trades models.TradeService,
	orderbookService models.OrderbookService,
	log models.Logger,
) (*Engine, error) {
	var err error
	engine := &Engine{
		MarketHandlerMap: make(map[models.Symbol]*MarketHandler),
		markets:          markets,
		trades:           trades,
		orderbook:        orderbookService,
		logger:           log,
		orders:           orders,
	}
	if markets != nil {
		err = engine.RegisterMarketHandlers(ctx)
		if err != nil {
			return nil, fmt.Errorf("RegisterMarketHandlers: %v", err)
		}
	}

	return engine, err
}

func (e *Engine) RegisterMarketHandlers(ctx context.Context) error {
	markets, err := e.markets.GetMarkets()
	if err != nil {
		return fmt.Errorf("markets.GetMarkets(): %v", err)
	}

	for _, market := range markets {
		err = e.RegisterMarketHandler(ctx, market)
		if err != nil {
			return fmt.Errorf("RegisterMarketHandler %s: %v", market.ID, err)
		}
	}

	return nil
}

func (e *Engine) RegisterMarketHandler(ctx context.Context, market *models.Market) error {
	mh, err := NewMarketHandler(e, market)
	if err != nil {
		return fmt.Errorf("NewMarketHandler: %v", err)
	}
	e.MarketHandlerMap[market.ID] = mh

	// open orders
	orders, err := e.orders.GetOrdersByPair(ctx, market.ID.String())
	if err != nil {
		return fmt.Errorf("GetOrdersByPair: %v", err)
	}

	for _, order := range orders {
		switch order.Status {
		case models.OrderStatusOpen, models.OrderStatusPartiallyCompleted:
			err = e.RegisterOrder(order)
			if err != nil {
				continue
			}
		case models.OrderStatusPendingTriggerPrice:
			err = e.RegisterStopOrder(order)
			if err != nil {
				e.logger.Error("", "RegisterStopOrder %v", err)

				continue
			}
			e.logger.Debug("engine", "RegisterStopOrder: %v", order)
		}
	}

	e.logger.Info("book", "Registered %s", mh.id)

	return nil
}

// RegisterOrder load a new order into the engine. If there is no market handler, an error occurs because the pair must be listed
func (e *Engine) RegisterOrder(order *models.Order) error {
	e.lock.Lock()
	defer e.lock.Unlock()

	handler, exist := e.MarketHandlerMap[order.Symbol]
	if !exist {
		return fmt.Errorf("market handler %s is not registered", order.Symbol)
	}

	return handler.orderbook.InsertOrder(order)
}

// RegisterStopOrder load a new order into the stop order listener. If there is no market handler, an error occurs because the pair must be listed.
func (e *Engine) RegisterStopOrder(order *models.Order) error {
	e.lock.Lock()
	defer e.lock.Unlock()

	handler, exist := e.MarketHandlerMap[order.Symbol]
	if !exist {
		return fmt.Errorf("market handler %s is not registered", order.Symbol)
	}

	return handler.stopListener.InsertOrder(order)
}
