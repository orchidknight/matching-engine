package models

import (
	"fmt"
	"github.com/google/uuid"
	"time"

	"github.com/shopspring/decimal"
)

type Side string

func (o Side) String() string {
	return string(o)
}

func (o Side) Opposite() Side {
	if o == Buy {
		return Sell
	}

	return Buy
}

const (
	Unspecified Side = "Unspecified"
	Buy         Side = "Buy"
	Sell        Side = "Sell"
)

type OrderType string

func (o OrderType) String() string {
	return string(o)
}

const (
	OrderTypeUnspecified OrderType = "Unspecified"
	OrderTypeLimit       OrderType = "Limit"
	OrderTypeMarket      OrderType = "Market"
	OrderTypeStopLimit   OrderType = "StopLimit"
	OrderTypeStopMarket  OrderType = "StopMarket"
)

var OrderTypeMap = map[OrderType]int32{
	OrderTypeUnspecified: 0,
	OrderTypeMarket:      1,
	OrderTypeLimit:       2,
	OrderTypeStopLimit:   3,
	OrderTypeStopMarket:  4,
}

type OrderStatus string

func (os OrderStatus) String() string {
	return string(os)
}

const (
	OrderStatusUnspecified         OrderStatus = "Unspecified"
	OrderStatusNew                 OrderStatus = "New"
	OrderStatusTriggered           OrderStatus = "Triggered"
	OrderStatusOpen                OrderStatus = "Open"
	OrderStatusPartiallyCompleted  OrderStatus = "PartiallyCompleted"
	OrderStatusCompleted           OrderStatus = "Completed"
	OrderStatusCanceled            OrderStatus = "Canceled"
	OrderStatusRejected            OrderStatus = "Rejected"
	OrderStatusPendingTriggerPrice OrderStatus = "PendingTriggerPrice"
	OrderStatusPendingCancel       OrderStatus = "PendingCancel"
)

type Order struct {
	ID      uuid.UUID `json:"ID"`
	Account string    `json:"account"`

	Symbol         Symbol       `json:"symbol"`
	Type           OrderType    `json:"type"`
	Side           Side         `json:"side"`
	Status         OrderStatus  `json:"status"`
	RejectedReason RejectReason `json:"rejectedReason"`

	Amount          decimal.Decimal `json:"amount"`
	AvailableAmount decimal.Decimal `json:"availableAmount"`
	ExecutedAmount  decimal.Decimal `json:"executedAmount"`
	CanceledAmount  decimal.Decimal `json:"canceledAmount"`
	AvailableTotal  decimal.Decimal `json:"availableTotal"`
	ExecutedTotal   decimal.Decimal `json:"executedTotal"`

	Price           decimal.Decimal `json:"price"`
	ActivationPrice decimal.Decimal `json:"stopLimitPrice"`
	ActivationType  ActivationType  `json:"activationType"`
	AvgPrice        decimal.Decimal `json:"avgPrice"`

	LastTrade *Trade

	CreatedAt time.Time `json:"createdAt"`
}

func (o *Order) Total() decimal.Decimal {
	return o.AvailableAmount.Mul(o.Price)
}

func (o *Order) TotalExecuted() decimal.Decimal {
	return o.ExecutedAmount.Mul(o.AvgPrice)
}

func (o *Order) IsNewStopOrder() bool {
	return (o.Type == OrderTypeStopMarket || o.Type == OrderTypeStopLimit) && o.Status == OrderStatusNew
}

type MatchedOrder struct {
	Order         *Order          `json:"order"`
	IsDone        bool            `json:"isDone"`
	MatchedAmount decimal.Decimal `json:"matchedAmount"`
}

type OrderResponse struct {
	Symbol        Symbol              `json:"pair"`
	InitialOrder  *TakerOrderResult   `json:"initial_order"`
	MatchedOrders []*MakerOrderResult `json:"matched_orders"`
	LastPrice     *decimal.Decimal    `json:"last_price"`
}

func (or *OrderResponse) String() string {
	var matchedOrders []string
	for _, mo := range or.MatchedOrders {
		matchedOrders = append(matchedOrders, fmt.Sprintf("< %d %s %v %v >", mo.Order.ID, mo.Order.Side, mo.Order.Price, mo.Order.Amount))
	}

	return fmt.Sprintf("%s Taker: %d %s %v %v Makers: %v", or.InitialOrder.Order.Symbol, or.InitialOrder.Order.ID, or.InitialOrder.Order.Side, or.InitialOrder.Order.Price, or.InitialOrder.Order.Amount, matchedOrders)
}

type TakerOrderResult struct {
	Order *Order `json:"order"`
}

type MakerOrderResult struct {
	Order *Order `json:"order"`
	Trade *Trade `json:"trade"`
}

type OrderUpdate struct {
	ID              uuid.UUID
	Status          OrderStatus
	AvgPrice        decimal.Decimal
	AvailableAmount decimal.Decimal
	ExecutedAmount  decimal.Decimal
	CanceledAmount  decimal.Decimal
	ExecutedTotal   decimal.Decimal
	AvailableTotal  decimal.Decimal
	Description     string
	ActivationType  ActivationType
}

func (o *OrderUpdate) String() string {
	return fmt.Sprintf("OrderUpdate-%s %s ActivationType: %s AvgPrice: %v Available: %v Executed: %v Canceled: %v",
		o.ID, o.Status, o.ActivationType, o.AvgPrice, o.AvailableAmount, o.ExecutedAmount, o.CanceledAmount)
}

func (o *Order) Cancel() {
	o.CanceledAmount = o.CanceledAmount.Add(o.AvailableAmount)
	o.AvailableAmount = decimal.Zero
	o.Status = OrderStatusCanceled
}

func (o *Order) Reject(rejectReason RejectReason) {
	o.Status = OrderStatusRejected
	o.RejectedReason = rejectReason
}

func (o *Order) ApplyUpdate(u *OrderUpdate) {
	o.Status = u.Status
	o.AvailableAmount = u.AvailableAmount
	o.ExecutedAmount = u.ExecutedAmount
	o.CanceledAmount = u.CanceledAmount
	o.ExecutedTotal = o.ExecutedTotal.Add(u.ExecutedTotal)
	o.AvailableTotal = u.AvailableTotal
	u.ExecutedTotal = o.ExecutedTotal

	if u.ActivationType != "" {
		o.ActivationType = u.ActivationType
	}

	if o.ExecutedAmount.Equal(decimal.NewFromUint64(0)) {
		o.AvgPrice = decimal.NewFromUint64(0)
		u.AvgPrice = decimal.NewFromUint64(0)
	} else {
		o.AvgPrice = o.ExecutedTotal.Div(o.ExecutedAmount)
		u.AvgPrice = o.AvgPrice
	}
}

type ActivationType string

func (at ActivationType) String() string {
	return string(at)
}

const (
	ActivationTypeUnspecified ActivationType = "Unspecified"
	ActivationTypeLess        ActivationType = "less"
	ActivationTypeMore        ActivationType = "more"
)

type NewOrderRequest struct {
	User            string  `json:"user"`
	Symbol          string  `json:"pair"`
	Type            string  `json:"type"`
	Side            string  `json:"side"`
	ActivationPrice float64 `json:"activationPrice"`
	Price           float64 `json:"price"`
	Size            float64 `json:"size"`
	Amount          float64 `json:"amount"`
}

func NewOrder(r NewOrderRequest) (*Order, error) {
	orderType := OrderType(r.Type)
	_, ok := OrderTypeMap[orderType]
	if !ok {
		return nil, fmt.Errorf("unknown order type")
	}

	symbol, err := NewSymbol(r.Symbol)
	if err != nil {
		return nil, err
	}

	var orderSide Side

	switch r.Side {
	case "buy":
		orderSide = Buy
	case "sell":
		orderSide = Sell
	default:
		return nil, fmt.Errorf("unknown order side")
	}

	return &Order{
		ID:              uuid.New(),
		Account:         r.User,
		Symbol:          symbol,
		Type:            orderType,
		Side:            orderSide,
		Status:          OrderStatusNew,
		Amount:          decimal.NewFromFloat(r.Amount),
		AvailableAmount: decimal.NewFromFloat(r.Amount),
		Price:           decimal.NewFromFloat(r.Price),
		ExecutedTotal:   decimal.NewFromFloat(r.Size),
		AvailableTotal:  decimal.NewFromFloat(r.Size),
		ActivationPrice: decimal.NewFromFloat(r.ActivationPrice),
		CreatedAt:       time.Now().UTC(),
	}, nil
}
