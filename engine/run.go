package engine

import (
	"context"
	"github.com/orchidknight/matching-engine/models"
)

func (e *Engine) Run(ctx context.Context) error {
	var err error

	e.logger.Debug("engine", "Matching engine start running...")

	for _, mh := range e.MarketHandlerMap {
		go func() {
			err = mh.Run(ctx)
			if err != nil {
				e.logger.Error("engine", "MarketHandler.Run: %v", err)
			}
		}()
	}

	return nil
}

func (e *Engine) ConsumeOrder(order *models.Order) {
	e.logger.Debug("RECEIVED", "Incoming: %v", order)

	mh, ok := e.MarketHandlerMap[order.Symbol]
	if !ok {
		return
	}

	mh.ConsumeOrder(order)
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

	mh.logger.Debug("engine", "Canceled: %d", order.ID)

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

		return nil
	}

	return nil
}

func (mh *MarketHandler) processToOrderbook(ctx context.Context, o *models.Order) error {
	var err error

	orderUpdate := &models.OrderUpdate{
		ID:              o.ID,
		Status:          models.OrderStatusNew,
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

	return nil
}

func (mh *MarketHandler) ConsumeOrder(o *models.Order) {
	mh.incomingOrders <- o
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

		return nil
	}

	order.Reject(models.RejectReasonNotEnoughLiquidity)

	err := mh.engine.orders.Reject(ctx, order)
	if err != nil {
		mh.logger.Error("orders", "Reject: %v", err)
	}

	mh.logger.Debug("engine", "Order <%d> has been rejected, not enough liquidity", order.ID)

	return nil
}
