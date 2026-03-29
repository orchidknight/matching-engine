package engine

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"sync"

	"github.com/petar/GoLLRB/llrb"
	"github.com/shopspring/decimal"

	"github.com/orchidknight/matching-engine/models"
)

// StopListener A service that starts and listens to the last price, selects stop-limit orders, and sends them to the channel if the price triggers their stop price.
// Input data: channel with orders, channel with price
// Output data: write new orders to the engine channel when their price is reached.
type StopListener interface {
	Run(ctx context.Context)
	RemoveOrder(o *models.Order) error
	InsertOrder(o *models.Order) error
}
type StopOrderListener struct {
	marketID          models.Symbol // example BTC-USDT
	lessTree          *llrb.LLRB
	moreTree          *llrb.LLRB
	incomingPriceChan chan models.Price
	outcomeOrderChan  chan *models.Order
	lastPrice         decimal.Decimal
	orders            models.OrderService
	lock              sync.RWMutex
	log               models.Logger
}

// NewStopListener return a new book
func NewStopListener(marketID models.Symbol, priceChan chan models.Price, out chan *models.Order, or models.OrderService, log models.Logger) *StopOrderListener {
	stopListener := &StopOrderListener{
		marketID:          marketID,
		lessTree:          llrb.New(),
		moreTree:          llrb.New(),
		incomingPriceChan: priceChan,
		outcomeOrderChan:  out,
		lock:              sync.RWMutex{},
		orders:            or,
		log:               log,
	}

	return stopListener
}

func (sl *StopOrderListener) Run(ctx context.Context) {
	sl.log.Debug("sol", "Started stop limit listener for %s", sl.marketID)
	// go to the stop order price level in the tree
	// place the order in the node
	// listen for the price, receive it, check the node with that price, and if there are orders, send it to the matching engine and delete it from the tree
	for {
		select {
		case <-ctx.Done():
			return
		case currentPrice := <-sl.incomingPriceChan:
			var triggeredOrders []*models.Order
			if !sl.lastPrice.Equal(currentPrice.LastPrice) {
				triggeredOrders = sl.getTriggeredOrders(currentPrice)
			}

			if len(triggeredOrders) == 0 {
				continue
			}

			sl.processTriggeredOrders(ctx, triggeredOrders)
		}
	}
}

func (sl *StopOrderListener) processTriggeredOrders(ctx context.Context, triggeredOrders []*models.Order) {
	for _, triggeredOrder := range triggeredOrders {
		dbOrder, err := sl.orders.GetOrderByID(ctx, triggeredOrder.ID)
		if err != nil {
			sl.log.Error("sol", "GetOrderByID: %v", err)
		}

		dbOrder.Status = models.OrderStatusTriggered
		err = sl.orders.UpdateOrder(ctx, dbOrder)
		if err != nil {
			sl.log.Error("sol", "UpdateOrder: %v", err)
		}

		sl.log.Debug("sol", "Sent triggered stop order to engine: %v", dbOrder)
		sl.outcomeOrderChan <- dbOrder
	}
}

func (sl *StopOrderListener) getTriggeredOrders(currentPrice models.Price) []*models.Order {
	var stopOrders []*models.Order

	lessOrders, err := sl.LessOrders(currentPrice.LastPrice)
	if err != nil {
		sl.log.Warn("sol", "less orders")
	}
	moreOrders, err := sl.MoreOrders(currentPrice.LastPrice)
	if err != nil {
		sl.log.Warn("sol", "more orders")
	}
	stopOrders = append(stopOrders, lessOrders...)
	stopOrders = append(stopOrders, moreOrders...)
	for _, o := range stopOrders {
		err = sl.RemoveOrder(o)
		if err != nil {
			sl.log.Error("sol", "RemoveOrder: %v", err)
		}
	}
	sl.log.Debug("sol", "%s price has changed from %v to %v. Got %d stop orders",
		sl.marketID, sl.lastPrice, currentPrice.LastPrice, len(stopOrders))
	sl.lastPrice = currentPrice.LastPrice

	return stopOrders
}

func (sl *StopOrderListener) InsertOrder(order *models.Order) error {
	sl.lock.Lock()
	defer sl.lock.Unlock()
	var tree *llrb.LLRB
	switch order.ActivationType {
	case models.ActivationTypeLess:
		tree = sl.lessTree
	case models.ActivationTypeMore:
		tree = sl.moreTree
	default:
		return nil
	}

	price := tree.Get(newPriceNode(order.ActivationPrice))
	if price == nil {
		price = newPriceNode(order.ActivationPrice)
		tree.InsertNoReplace(price)
	}

	err := price.(*PriceNode).InsertOrder(*order)
	if err != nil {
		return err
	}

	return nil
}

func (sl *StopOrderListener) LessOrders(fromPrice decimal.Decimal) ([]*models.Order, error) {
	var orders []*models.Order
	var err error
	var orderIDs []uuid.UUID

	priceIterator := func(i llrb.Item) bool {
		priceNode := i.(*PriceNode)
		sl.log.Debug("sol", "Check price node %v with %d orders", priceNode.price, priceNode.orderMap.Len())
		iter := priceNode.orderMap.IterFunc()
		for kv, ok := iter(); ok; kv, ok = iter() {
			order := kv.Value.(models.Order)
			if fromPrice.LessThanOrEqual(order.ActivationPrice) {
				orderIDs = append(orderIDs, order.ID)
				orders = append(orders, &order)
			}
		}

		return true
	}
	sl.lessTree.AscendGreaterOrEqual(newPriceNode(fromPrice), priceIterator)
	sl.log.Debug("sol", "MoreOrders: find more orders to %v: %s", fromPrice, orderIDs)

	return orders, err
}
func (sl *StopOrderListener) MoreOrders(fromPrice decimal.Decimal) ([]*models.Order, error) {
	var orders []*models.Order
	var err error
	var orderIDs []uuid.UUID

	sl.log.Debug("sol", "MoreOrders: find more orders to %v", fromPrice)
	priceIterator := func(i llrb.Item) bool {
		priceNode := i.(*PriceNode)
		sl.log.Debug("sol", "Got price node %v with %d orders", priceNode.price, priceNode.orderMap.Len())
		iter := priceNode.orderMap.IterFunc()
		for kv, ok := iter(); ok; kv, ok = iter() {
			order := kv.Value.(models.Order)
			if fromPrice.GreaterThanOrEqual(order.ActivationPrice) {
				orderIDs = append(orderIDs, order.ID)
				orders = append(orders, &order)
			}
		}

		return true
	}
	sl.moreTree.DescendLessOrEqual(newPriceNode(fromPrice), priceIterator)
	sl.log.Debug("sol", "MoreOrders: find more orders to %v: %s", fromPrice, orderIDs)

	return orders, err
}

func (sl *StopOrderListener) RemoveOrder(o *models.Order) error {
	var priceNode *PriceNode
	var tree *llrb.LLRB

	sl.lock.Lock()
	defer sl.lock.Unlock()

	switch o.ActivationType {
	case models.ActivationTypeLess:
		tree = sl.lessTree
	case models.ActivationTypeMore:
		tree = sl.moreTree
	}

	node := tree.Get(newPriceNode(o.ActivationPrice))
	if node == nil {
		return nil
	}

	priceNode = node.(*PriceNode)
	err := priceNode.RemoveOrder(o.ID)
	if err != nil {
		return fmt.Errorf("RemoveOrder %s activation: %s %v, error: %v", o.ID.String(), o.ActivationType, o.ActivationPrice, err)
	}
	if priceNode.Len() == 0 {
		tree.Delete(priceNode)
	}

	return err
}
