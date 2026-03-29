package engine

import (
	"fmt"

	"github.com/cevaris/ordered_map"
	"github.com/google/uuid"
	"github.com/petar/GoLLRB/llrb"
	"github.com/shopspring/decimal"

	"github.com/orchidknight/matching-engine/models"
)

type PriceNode struct {
	price       decimal.Decimal
	totalAmount decimal.Decimal
	orderMap    *ordered_map.OrderedMap
}

func newPriceNode(price decimal.Decimal) *PriceNode {
	return &PriceNode{
		price:       price,
		totalAmount: decimal.NewFromUint64(0),
		orderMap:    ordered_map.NewOrderedMap(),
	}
}

func (pn *PriceNode) String() string {
	return fmt.Sprintf("Price:%v TotalAmount: %v OrdersCount: %d", pn.price, pn.totalAmount, pn.orderMap.Len())
}

// InsertOrder - Insert new order to price node
func (pn *PriceNode) InsertOrder(order models.Order) error {
	if _, ok := pn.orderMap.Get(order.ID); ok {
		return fmt.Errorf("can't add order which is already in this PriceNode. PriceNode: %v, orderID: %s", pn.price, order.ID.String())
	}
	pn.orderMap.Set(order.ID, order)
	pn.totalAmount = pn.totalAmount.Add(order.AvailableAmount)

	return nil
}

// GetOrder - Read
func (pn *PriceNode) GetOrder(id uint64) (order models.Order, exist bool) {
	orderItem, exist := pn.orderMap.Get(id)
	if !exist {
		return models.Order{}, exist
	}

	return orderItem.(models.Order), exist
}

// UpdateOrder - Update order amounts in price node
func (pn *PriceNode) UpdateOrder(id uuid.UUID, changeAmount decimal.Decimal) error {
	data, ok := pn.orderMap.Get(id)
	if !ok {
		return fmt.Errorf("can't update order which is not in this PriceNode. PriceNode: %v", pn.price)
	}

	order := data.(models.Order)
	order.AvailableAmount = order.AvailableAmount.Sub(changeAmount)
	pn.totalAmount = pn.totalAmount.Sub(changeAmount)
	pn.orderMap.Set(id, order)

	return nil
}

// RemoveOrder - Delete order from price node
func (pn *PriceNode) RemoveOrder(id uuid.UUID) error {
	orderItem, ok := pn.orderMap.Get(id)
	if !ok {
		return fmt.Errorf("can't remove order which is not in this PriceNode. PriceNode: %v", pn.price)
	}
	order := orderItem.(models.Order)
	pn.orderMap.Delete(order.ID)
	pn.totalAmount = pn.totalAmount.Sub(order.AvailableAmount)

	return nil
}

func (pn *PriceNode) Len() int {
	return pn.orderMap.Len()
}

// Less - required method for a price node to be an item in the orderbookService tree
func (pn *PriceNode) Less(item llrb.Item) bool {
	another := item.(*PriceNode)

	return pn.price.LessThan(another.price)
}
