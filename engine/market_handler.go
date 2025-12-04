package engine

import (
	"context"
	"fmt"

	"github.com/orchidknight/matching-engine/models"
)

type MarketHandler struct {
	id                   models.Symbol
	marketAmountDecimals int
	market               *models.Market
	orderbook            *Orderbook
	incomingOrders       chan *models.Order
	engine               *Engine
	stopListener         StopListener
	logger               models.Logger
}

func (mh *MarketHandler) String() string {
	return fmt.Sprintf("MH id: %s market: %v amountDecimals: %d orderbookService: %v ", mh.id, mh.market, mh.marketAmountDecimals, mh.orderbook)
}

func NewMarketHandler(engine *Engine, market *models.Market) (*MarketHandler, error) {
	marketOrderbook := NewOrderbook(market.ID, engine.orderbook, engine.orders, engine.markets, engine.trades, engine.logger)
	incomingOrders := make(chan *models.Order, 10000)
	priceChan := make(chan models.Price, 100)
	marketHandler := MarketHandler{
		id:                   market.ID,
		market:               market,
		orderbook:            marketOrderbook,
		engine:               engine,
		marketAmountDecimals: market.QuoteAsset.CalculationPrecision,
		logger:               engine.logger,
		incomingOrders:       incomingOrders,
		stopListener:         NewStopListener(market.ID, priceChan, incomingOrders, engine.orders, engine.logger),
	}

	return &marketHandler, nil
}

func (mh *MarketHandler) Run(ctx context.Context) error {
	mh.logger.Debug("engine", "Market handler %s running...", mh.id)

	for {
		select {
		case <-ctx.Done():
			close(mh.incomingOrders)

			return nil
		case o := <-mh.incomingOrders:
			err := mh.ProcessOrder(ctx, o)
			if err != nil {
				mh.logger.Error("engine", "Cant process %d : %v", o.ID, err)
			}
		}
	}
}
