package engine

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/petar/GoLLRB/llrb"
	"github.com/shopspring/decimal"

	"github.com/orchidknight/matching-engine/models"
)

var Zero = decimal.Zero

// nolint nestif
func (book *Orderbook) Match(ctx context.Context, takerOrder *models.Order) *models.OrderResponse {
	var err error
	var trade *models.Trade
	var trades []*models.Trade

	response := &models.OrderResponse{
		Symbol: takerOrder.Symbol,
	}

	var result *MatchResult
	if !takerOrder.AvailableAmount.Equal(Zero) {
		result = book.MatchOrderWithAmount(takerOrder)
	} else if !takerOrder.AvailableTotal.Equal(Zero) {
		result = book.MatchOrderWithTotal(takerOrder)
	}

	switch takerOrder.Type {
	case models.OrderTypeMarket, models.OrderTypeStopMarket:
		if len(result.MatchedOrders) == 0 {
			book.log.Debug("book", "Market order got 0 matches, reject")
			takerOrder.Reject(models.RejectReasonNoMatches)
			err = book.orders.Reject(ctx, takerOrder)
			if err != nil {
				book.log.Error("book", "Reject: %v", err)
			}

			response.InitialOrder = takerOrder

			return response
		}

		orderUpdate := &models.OrderUpdate{
			ID:              takerOrder.ID,
			Status:          models.OrderStatusCompleted,
			AvailableAmount: result.AmountLeft,
			AvailableTotal:  result.TotalLeft,
			ExecutedAmount:  result.AmountDone,
			CanceledAmount:  Zero,
			ExecutedTotal:   result.TotalDone,
		}
		takerOrder.ApplyUpdate(orderUpdate)

	case models.OrderTypeLimit, models.OrderTypeStopLimit:
		// If the limit or stop limit order is not completely filled, we send it to the order book
		if !result.IsDone && result.AmountLeft.GreaterThan(Zero) && result.AmountDone.GreaterThan(Zero) {
			orderUpdate := &models.OrderUpdate{
				ID:              takerOrder.ID,
				Status:          models.OrderStatusPartiallyCompleted,
				AvailableAmount: result.AmountLeft,
				ExecutedAmount:  result.AmountDone,
				CanceledAmount:  Zero,
				ExecutedTotal:   result.TotalDone,
			}
			takerOrder.ApplyUpdate(orderUpdate)
			err = book.InsertOrder(takerOrder)
			if err != nil {
				book.log.Error("book", "Insert order: %v", err)
			}

			err = book.orders.UpdateOrder(ctx, takerOrder)
			if err != nil {
				book.log.Error("book", "UpdateOrder: %v", err)
			}
			response.InitialOrder = takerOrder
		}
		if !result.IsDone && result.AmountLeft.Equal(Zero) {
			orderUpdate := &models.OrderUpdate{
				ID:              takerOrder.ID,
				Status:          models.OrderStatusOpen,
				AvailableAmount: result.AmountLeft,
				ExecutedAmount:  result.AmountDone,
				CanceledAmount:  Zero,
				ExecutedTotal:   result.TotalDone,
			}
			takerOrder.ApplyUpdate(orderUpdate)
			err = book.InsertOrder(takerOrder)
			if err != nil {
				book.log.Error("book", "InsertOrder: %v", err)
			}
		}
		if result.IsDone {
			orderUpdate := &models.OrderUpdate{
				ID:              takerOrder.ID,
				Status:          models.OrderStatusCompleted,
				AvailableAmount: Zero,
				ExecutedAmount:  result.AmountDone,
				CanceledAmount:  Zero,
				ExecutedTotal:   result.TotalDone,
			}
			takerOrder.ApplyUpdate(orderUpdate)
		}
	}

	for _, matchedOrder := range result.MatchedOrders {
		// filled out the order and removed it from the order book
		if matchedOrder.IsDone {
			err = book.RemoveOrder(matchedOrder.Order)
			if err != nil {
				book.log.Error("book", "RemoveOrder: %v", err)
			}
			orderUpdate := &models.OrderUpdate{
				ID:              matchedOrder.Order.ID,
				Status:          models.OrderStatusCompleted,
				AvailableAmount: Zero,
				ExecutedAmount:  matchedOrder.Order.Amount,
				CanceledAmount:  Zero,
				ExecutedTotal:   matchedOrder.MatchedAmount.Mul(matchedOrder.Order.Price),
			}
			trade = &models.Trade{
				ID:           fmt.Sprintf("%d_%d", takerOrder.ID, matchedOrder.Order.ID),
				TakerID:      takerOrder.Account,
				MakerID:      matchedOrder.Order.Account,
				Symbol:       takerOrder.Symbol,
				TakerOrderID: takerOrder.ID,
				MakerOrderID: matchedOrder.Order.ID,
				Amount:       matchedOrder.MatchedAmount,
				Price:        matchedOrder.Order.Price,
				TakerSide:    takerOrder.Side,
				CreatedAt:    time.Now().UTC(),
			}

			matchedOrder.Order.ApplyUpdate(orderUpdate)
			matchedOrder.Order.LastTrade = trade

			if err = book.trades.ConsumeTrade(ctx, trade); err != nil {
				book.log.Error("tradeService", "ConsumeTrade: %v", err)
			}
			err = book.orderbookService.ConsumeTrade(ctx, trade)
			if err != nil {
				book.log.Error("orderbookService", "ConsumeTrade: %v", err)
			}

			err = book.orders.UpdateOrder(ctx, matchedOrder.Order)
			if err != nil {
				book.log.Error("book", "UpdateOrder: %v", err)
			}

			response.MatchedOrders = append(response.MatchedOrders, &models.MatchedOrderResult{
				Order: matchedOrder.Order,
				Trade: trade,
			})

			trades = append(trades, trade)
		} else {
			// частично заполнили ордер
			err = book.ChangeOrder(ctx, matchedOrder.Order, matchedOrder.MatchedAmount)
			if err != nil {
				book.log.Error("book", "ChangeOrder: %v", err)
			}

			orderUpdate := &models.OrderUpdate{
				ID:              matchedOrder.Order.ID,
				Status:          models.OrderStatusPartiallyCompleted,
				AvailableAmount: matchedOrder.Order.AvailableAmount.Sub(matchedOrder.MatchedAmount),
				ExecutedAmount:  matchedOrder.Order.ExecutedAmount.Add(matchedOrder.MatchedAmount),
				CanceledAmount:  Zero,
				ExecutedTotal:   matchedOrder.MatchedAmount.Mul(matchedOrder.Order.Price),
			}
			trade = &models.Trade{
				ID:           fmt.Sprintf("%d_%d", takerOrder.ID, matchedOrder.Order.ID),
				TakerID:      takerOrder.Account,
				MakerID:      matchedOrder.Order.Account,
				Symbol:       takerOrder.Symbol,
				TakerOrderID: takerOrder.ID,
				MakerOrderID: matchedOrder.Order.ID,
				Amount:       matchedOrder.MatchedAmount,
				Price:        matchedOrder.Order.Price,
				TakerSide:    takerOrder.Side,
				CreatedAt:    time.Now().UTC(),
			}

			matchedOrder.Order.ApplyUpdate(orderUpdate)
			matchedOrder.Order.LastTrade = trade

			err = book.orders.UpdateOrder(ctx, matchedOrder.Order)
			if err != nil {
				book.log.Error("book", "UpdateOrder: %v", err)
			}

			response.MatchedOrders = append(response.MatchedOrders, &models.MatchedOrderResult{
				Order: matchedOrder.Order,
				Trade: trade,
			})

			if err = book.trades.ConsumeTrade(ctx, trade); err != nil {
				book.log.Error("tradeService", "ConsumeTrade: %v", err)
			}

			err := book.orderbookService.ConsumeTrade(ctx, trade)
			if err != nil {
				book.log.Error("orderbookService", "ConsumeTrade: %v", err)
			}

			trades = append(trades, trade)
		}
	}

	lastPrice := Zero
	if len(trades) > 0 {
		lastTrade := trades[len(trades)-1]
		lastPrice = lastTrade.Price
		takerOrder.LastTrade = lastTrade
		response.LastPrice = &lastPrice

		err := book.orders.UpdateOrder(ctx, takerOrder)
		if err != nil {
			book.log.Error("orders", "UpdateOrder: %v", err)
		}

		response.InitialOrder = takerOrder

		market, _ := book.markets.GetMarketByID(book.market.String())
		if market != nil {
			market.LastSpotPrice = lastPrice
			err = book.markets.UpdateMarket(market)
			if err != nil {
				book.log.Error("book", "UpdateMarket: %v", err)
			}
		}
	}

	return response
}

func checkLimitPrice(takerOrder *models.Order, price decimal.Decimal) bool {
	if takerOrder.Type == models.OrderTypeMarket || takerOrder.Type == models.OrderTypeStopMarket {
		return true
	}
	if takerOrder.Side == models.Buy && price.GreaterThan(takerOrder.Price) {
		return false
	} else if takerOrder.Side == models.Sell && price.LessThan(takerOrder.Price) {
		return false
	}

	return true
}

// nolint revive
func (book *Orderbook) MatchOrderWithAmount(takerOrder *models.Order) *MatchResult {
	book.lock.Lock()
	defer book.lock.Unlock()

	matchedResult := make([]*models.MatchedOrder, 0)

	matchedTotal := Zero
	matchedAmount := Zero
	leftAmount := takerOrder.AvailableAmount

	iteratorFunc := func(i llrb.Item) bool {
		pl := i.(*PriceNode)

		if !checkLimitPrice(takerOrder, pl.price) {
			return false
		}

		iter := pl.orderMap.IterFunc()
		for kv, ok := iter(); ok; kv, ok = iter() {
			// break when no leftAmount
			if leftAmount.LessThanOrEqual(Zero) {
				return false
			}

			makerOrder := kv.Value.(models.Order)

			// do not match same owner orders(depends on exchange global settings and order flow)
			if makerOrder.Account == takerOrder.Account {
				continue
			}

			matchedItem := &models.MatchedOrder{
				Order: &makerOrder,
			}

			switch takerOrder.Side {
			case models.Buy:
				if leftAmount.Sub(makerOrder.AvailableAmount).GreaterThanOrEqual(Zero) {
					leftAmount = leftAmount.Sub(makerOrder.AvailableAmount)
					matchedItem.MatchedAmount = makerOrder.AvailableAmount
					matchedItem.IsDone = true
					matchedTotal = matchedTotal.Add(matchedItem.MatchedAmount.Mul(makerOrder.Price))
					matchedAmount = matchedAmount.Add(matchedItem.MatchedAmount)
				} else {
					matchedItem.MatchedAmount = leftAmount
					matchedItem.IsDone = false
					matchedTotal = matchedTotal.Add(matchedItem.MatchedAmount.Mul(makerOrder.Price))
					leftAmount = Zero
					matchedAmount = matchedAmount.Add(matchedItem.MatchedAmount)
				}
			case models.Sell:
				if leftAmount.Sub(makerOrder.AvailableAmount).GreaterThanOrEqual(Zero) {
					matchedItem.MatchedAmount = makerOrder.AvailableAmount
					matchedItem.IsDone = true
					leftAmount = leftAmount.Sub(makerOrder.AvailableAmount)
					matchedTotal = matchedTotal.Add(matchedItem.MatchedAmount.Mul(makerOrder.Price))
					matchedAmount = matchedAmount.Add(matchedItem.MatchedAmount)
				} else {
					matchedItem.MatchedAmount = leftAmount
					matchedItem.IsDone = false
					leftAmount = Zero
					matchedTotal = matchedTotal.Add(matchedItem.MatchedAmount.Mul(makerOrder.Price))
					matchedAmount = matchedAmount.Add(matchedItem.MatchedAmount)
				}
			default:
				return false
			}
			matchedResult = append(matchedResult, matchedItem)
		}

		return leftAmount.GreaterThanOrEqual(Zero)
	}

	switch takerOrder.Side {
	case models.Sell:
		book.bidsTree.DescendLessOrEqual(newPriceNode(decimal.NewFromUint64(math.MaxUint64)), iteratorFunc)
	case models.Buy:
		book.asksTree.AscendGreaterOrEqual(newPriceNode(Zero), iteratorFunc)
	}

	isDone := leftAmount.Equal(Zero)

	return &MatchResult{
		MatchedOrders: matchedResult,
		Order:         takerOrder,
		IsDone:        isDone,
		AmountLeft:    leftAmount,
		TotalDone:     matchedTotal,
		AmountDone:    matchedAmount,
	}
}

// nolint revive
func (book *Orderbook) MatchOrderWithTotal(takerOrder *models.Order) *MatchResult {
	book.lock.Lock()
	defer book.lock.Unlock()

	matchedResult := make([]*models.MatchedOrder, 0)

	matchedTotal := Zero
	matchedAmount := Zero
	leftTotal := takerOrder.AvailableTotal

	iteratorFunc := func(i llrb.Item) bool {
		pl := i.(*PriceNode)

		if !checkLimitPrice(takerOrder, pl.price) {
			return false
		}

		iter := pl.orderMap.IterFunc()
		for kv, ok := iter(); ok; kv, ok = iter() {
			// break when no leftAmount
			if leftTotal.LessThanOrEqual(Zero) {
				return false
			}

			makerOrder := kv.Value.(models.Order)
			// do not match same owner orders(depends on exchange global settings and order flow)
			if makerOrder.Account == takerOrder.Account {
				continue
			}

			matchedItem := &models.MatchedOrder{
				Order: &makerOrder,
			}

			switch takerOrder.Side {
			case models.Buy:
				if leftTotal.Sub(makerOrder.AvailableAmount.Mul(makerOrder.Price)).GreaterThanOrEqual(Zero) {
					leftTotal = leftTotal.Sub(makerOrder.AvailableAmount.Mul(makerOrder.Price))
					matchedItem.MatchedAmount = makerOrder.AvailableAmount
					matchedItem.IsDone = true
					matchedTotal = matchedTotal.Add(matchedItem.MatchedAmount.Mul(makerOrder.Price))
					matchedAmount = matchedAmount.Add(matchedItem.MatchedAmount)
				} else {
					matchedItem.MatchedAmount = leftTotal.Div(makerOrder.Price)
					matchedItem.IsDone = false
					matchedTotal = matchedTotal.Add(leftTotal)
					leftTotal = Zero
					matchedAmount = matchedAmount.Add(matchedItem.MatchedAmount)
				}
			case models.Sell:
				if leftTotal.Sub(makerOrder.AvailableAmount.Mul(makerOrder.Price)).GreaterThanOrEqual(Zero) {
					matchedItem.MatchedAmount = makerOrder.AvailableAmount
					matchedItem.IsDone = true
					leftTotal = leftTotal.Sub(makerOrder.AvailableAmount.Mul(makerOrder.Price))
					matchedTotal = matchedTotal.Add(matchedItem.MatchedAmount.Mul(makerOrder.Price))
					matchedAmount = matchedAmount.Add(matchedItem.MatchedAmount)
				} else {
					matchedItem.MatchedAmount = leftTotal.Div(makerOrder.Price)
					matchedItem.IsDone = false
					leftTotal = Zero
					matchedTotal = matchedTotal.Add(matchedItem.MatchedAmount.Mul(makerOrder.Price))
					matchedAmount = matchedAmount.Add(matchedItem.MatchedAmount)
				}
			default:
				return false
			}
			matchedResult = append(matchedResult, matchedItem)
		}

		return leftTotal.GreaterThanOrEqual(Zero)
	}

	switch takerOrder.Side {
	case models.Sell:
		book.bidsTree.DescendLessOrEqual(newPriceNode(decimal.NewFromUint64(math.MaxUint64)), iteratorFunc)
	case models.Buy:
		book.asksTree.AscendGreaterOrEqual(newPriceNode(Zero), iteratorFunc)
	}

	isDone := leftTotal.Equal(Zero)

	return &MatchResult{
		MatchedOrders: matchedResult,
		Order:         takerOrder,
		IsDone:        isDone,
		TotalLeft:     leftTotal,
		TotalDone:     matchedTotal,
		AmountDone:    matchedAmount,
	}
}
