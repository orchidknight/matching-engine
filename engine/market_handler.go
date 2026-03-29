package engine

import (
	"context"
	"fmt"
	"github.com/shopspring/decimal"
	"sync"

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

	done chan struct{}
	once sync.Once
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
		done:                 make(chan struct{}),
	}

	return &marketHandler, nil
}

func (mh *MarketHandler) Run(ctx context.Context) error {
	mh.logger.Debug("engine", "Market handler %s running...", mh.id)
	defer mh.once.Do(func() { close(mh.done) })

	for {
		select {
		case <-ctx.Done():
			return nil
		case o := <-mh.incomingOrders:
			err := mh.ProcessOrder(ctx, o)
			if err != nil {
				mh.logger.Error("engine", "Cant process %s : %v", o.ID.String(), err)
			}
		}
	}
}

// RegisterOrder load a new order into the market handler
func (mh *MarketHandler) RegisterOrder(order *models.Order) error {
	switch order.Status {
	case models.OrderStatusOpen, models.OrderStatusPartiallyCompleted:
		err := mh.orderbook.InsertOrder(order)
		if err != nil {
			return fmt.Errorf("orderbook.InsertOrder: %w", err)
		}
	case models.OrderStatusPendingTriggerPrice:
		err := mh.stopListener.InsertOrder(order)
		if err != nil {
			return fmt.Errorf("stopListener.InsertOrder: %w", err)
		}
	default:
		return fmt.Errorf("can't register order %s with order status %s", order.ID.String(), order.Status)
	}

	return nil
}

func (mh *MarketHandler) Reset() error {
	for i := false; !i; {
		item := mh.orderbook.asksTree.DeleteMin()
		if item == nil {
			i = true
		}
	}

	for i := false; !i; {
		item := mh.orderbook.bidsTree.DeleteMax()
		if item == nil {
			i = true
		}
	}

	mh.orderbook.asksVolume = decimal.Zero
	mh.orderbook.bidsVolume = decimal.Zero

	return nil
}

func (mh *MarketHandler) processCancel(ctx context.Context, o *models.Order) error {
	order, err := mh.engine.orders.GetOrderByID(ctx, o.ID)
	if err != nil {
		return err
	}

	if order == nil {
		return nil
	}

	err = mh.orderbook.RemoveOrder(order)
	if err != nil {
		return err
	}

	order.Cancel()

	err = mh.engine.orders.UpdateOrder(ctx, order)
	if err != nil {
		mh.logger.Error("engine", "CancelOrder: %v", err)

		return err
	}

	mh.engine.outcomingOrderResponses <- &models.OrderResponse{
		Symbol:       o.Symbol,
		InitialOrder: order,
	}

	mh.logger.Debug("engine", "Canceled: %s", order.ID.String())

	return nil
}

// nolint nestif
func (mh *MarketHandler) processStopOrder(ctx context.Context, order *models.Order) error {
	if order.Status == models.OrderStatusNew {
		if order.ActivationPrice.LessThan(mh.engine.trades.LastPrice(order.Symbol)) {
			order.ActivationType = models.ActivationTypeLess
		} else {
			order.ActivationType = models.ActivationTypeMore
		}
		err := mh.stopListener.InsertOrder(order)
		if err != nil {
			mh.logger.Error("sol", "InsertOrder: %v", err)

			return err
		}
		orderUpdate := &models.OrderUpdate{
			ID:              order.ID,
			Status:          models.OrderStatusPendingTriggerPrice,
			AvgPrice:        order.Price,
			AvailableAmount: order.AvailableAmount,
			CanceledAmount:  Zero,
			ExecutedAmount:  Zero,
			ActivationType:  order.ActivationType,
		}
		order.ApplyUpdate(orderUpdate)

		err = mh.engine.orders.UpdateOrder(ctx, order)
		if err != nil {
			mh.logger.Error("orders", "UpdateOrder: %v", err)
		}

		mh.engine.outcomingOrderResponses <- &models.OrderResponse{
			Symbol:       order.Symbol,
			InitialOrder: order,
		}

		return nil
	}

	return nil
}

func (mh *MarketHandler) processToOrderbook(ctx context.Context, o *models.Order) error {
	var err error

	orderUpdate := &models.OrderUpdate{
		ID:              o.ID,
		Status:          models.OrderStatusOpen,
		AvgPrice:        o.Price,
		AvailableAmount: o.AvailableAmount,
		ExecutedAmount:  Zero,
		CanceledAmount:  Zero,
	}
	o.ApplyUpdate(orderUpdate)

	err = mh.orderbook.InsertOrder(o)
	if err != nil {
		mh.logger.Error("engine", "InsertOrder: %v", err)

		return err
	}

	err = mh.engine.orders.UpdateOrder(ctx, o)
	if err != nil {
		mh.logger.Error("orders", "UpdateOrder: %v", err)
	}

	mh.engine.outcomingOrderResponses <- &models.OrderResponse{
		Symbol:       o.Symbol,
		InitialOrder: o,
	}

	return nil
}

func (mh *MarketHandler) ConsumeOrder(o *models.Order) bool {
	select {
	case <-mh.done:
		return false
	default:
	}

	select {
	case <-mh.done:
		return false
	case mh.incomingOrders <- o:
		return true
	}
}

func (mh *MarketHandler) ProcessOrder(ctx context.Context, o *models.Order) error {
	if o.Status == models.OrderStatusPendingCancel {
		return mh.processCancel(ctx, o)
	}

	if o.IsNewStopOrder() {
		return mh.processStopOrder(ctx, o)
	}

	if mh.orderbook.CanMatchImmediately(o) {
		return mh.processToMatch(ctx, o)
	}

	return mh.processToOrderbook(ctx, o)
}

func (mh *MarketHandler) processToMatch(ctx context.Context, order *models.Order) error {
	if mh.orderbook.EnoughLiquidity(order) {
		result := mh.orderbook.Match(ctx, order)
		mh.logger.Debug("engine", "MatchResult: %s", result.String())

		mh.engine.outcomingOrderResponses <- result

		return nil
	}

	order.Reject(models.RejectReasonNotEnoughLiquidity)

	err := mh.engine.orders.Reject(ctx, order)
	if err != nil {
		mh.logger.Error("orders", "Reject: %v", err)
	}

	mh.logger.Debug("engine", "Order <%s> has been rejected, not enough liquidity", order.ID.String())

	return nil
}
