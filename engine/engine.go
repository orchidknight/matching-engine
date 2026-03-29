package engine

import (
	"context"
	"fmt"
	"github.com/orchidknight/matching-engine/models"
	"sync"
)

type Engine struct {
	marketHandlers          map[models.Symbol]*MarketHandler
	outcomingOrderResponses chan *models.OrderResponse

	markets   models.MarketService
	orders    models.OrderService
	trades    models.TradeService
	orderbook models.OrderbookService

	lock   sync.RWMutex
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
		marketHandlers:          make(map[models.Symbol]*MarketHandler),
		outcomingOrderResponses: make(chan *models.OrderResponse, 10000),
		markets:                 markets,
		trades:                  trades,
		orderbook:               orderbookService,
		orders:                  orders,
		logger:                  log,
	}

	if markets != nil {
		err = engine.registerMarketHandlers(ctx)
		if err != nil {
			return nil, fmt.Errorf("registerMarketHandlers: %v", err)
		}
	}

	return engine, err
}

func (e *Engine) registerMarketHandlers(ctx context.Context) error {
	markets, err := e.markets.GetMarkets()
	if err != nil {
		return fmt.Errorf("markets.GetMarkets(): %v", err)
	}

	for _, market := range markets {
		err = e.registerMarketHandler(ctx, market)
		if err != nil {
			return fmt.Errorf("registerMarketHandler %s: %v", market.ID, err)
		}
	}

	return nil
}

func (e *Engine) registerMarketHandler(ctx context.Context, market *models.Market) error {
	mh, err := NewMarketHandler(e, market)
	if err != nil {
		return fmt.Errorf("NewMarketHandler: %v", err)
	}

	e.setMarketHandler(mh)

	orders, err := e.orders.GetOrdersByPair(ctx, market.ID.String())
	if err != nil {
		return fmt.Errorf("GetOrdersByPair: %v", err)
	}

	for _, order := range orders {
		err = mh.RegisterOrder(order)
		if err != nil {
			e.logger.Error("engine", "RegisterOrder: %v", err)

			continue
		}
	}

	e.logger.Info("book", "Registered %s", mh.id)

	return nil
}

func (e *Engine) setMarketHandler(mh *MarketHandler) {
	e.lock.Lock()
	defer e.lock.Unlock()

	e.marketHandlers[mh.id] = mh
}

func (e *Engine) getMarketHandler(id models.Symbol) (*MarketHandler, bool) {
	e.lock.RLock()
	defer e.lock.RUnlock()

	v, ok := e.marketHandlers[id]
	return v, ok
}

func (e *Engine) deleteMarketHandler(id models.Symbol) {
	e.lock.Lock()
	defer e.lock.Unlock()

	delete(e.marketHandlers, id)
}

func (e *Engine) GetLastOrderResponse() *models.OrderResponse {
	fmt.Println("GetLastOrderResponse")
	resp := <-e.outcomingOrderResponses
	return resp
}

func (e *Engine) Run(ctx context.Context) error {
	e.logger.Debug("engine", "Matching engine start running...")

	e.lock.RLock()
	handlers := make([]*MarketHandler, 0, len(e.marketHandlers))
	for _, mh := range e.marketHandlers {
		handlers = append(handlers, mh)
	}
	e.lock.RUnlock()

	if len(handlers) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(handlers))

	for _, h := range handlers {
		handler := h
		wg.Add(1)

		go func() {
			defer wg.Done()

			if err := handler.Run(ctx); err != nil {
				errCh <- err
				e.logger.Error("engine", "MarketHandler.Run: %v", err)
			}
		}()
	}

	wg.Wait()
	close(errCh)

	var firstErr error
	for err := range errCh {
		if firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

func (e *Engine) ConsumeOrder(order *models.Order) {
	e.logger.Debug("RECEIVED", "Incoming: %v", order)

	e.lock.RLock()
	defer e.lock.RUnlock()

	mh, ok := e.marketHandlers[order.Symbol]
	if !ok {
		order.Reject(models.RejectReasonWrongSymbol)

		e.outcomingOrderResponses <- &models.OrderResponse{
			Symbol:       order.Symbol,
			InitialOrder: order,
		}

		return
	}

	mh.ConsumeOrder(order)
}
