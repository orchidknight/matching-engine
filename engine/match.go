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

// divFloor divides total by price and floors the quotient to the base asset
// scale. Flooring (truncation toward zero) guarantees the taker is never
// credited base it did not pay for, and makes the result independent of the
// process-global decimal.DivisionPrecision.
func (book *Orderbook) divFloor(total, price decimal.Decimal) decimal.Decimal {
	return total.Div(price).Truncate(book.baseScale)
}

// nolint nestif
func (book *Orderbook) Match(ctx context.Context, takerOrder *models.Order) (*models.OrderResponse, error) {
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
	} else {
		takerOrder.Reject(models.RejectReasonWrongInput)
		err = book.orders.Reject(ctx, takerOrder)
		if err != nil {
			return nil, fmt.Errorf("reject wrong input order: %w", err)
		}

		response.InitialOrder = takerOrder

		return response, nil
	}

	switch takerOrder.Type {
	case models.OrderTypeMarket, models.OrderTypeStopMarket:
		if len(result.MatchedOrders) == 0 {
			book.log.Debug("book", "Market order got 0 matches, reject")
			takerOrder.Reject(models.RejectReasonNoMatches)
			err = book.orders.Reject(ctx, takerOrder)
			if err != nil {
				return nil, fmt.Errorf("reject market order with no matches: %w", err)
			}

			response.InitialOrder = takerOrder

			return response, nil
		}

		// IOC semantics: a market/stop-market order never rests. Any uncovered
		// remainder is canceled, not left in available under a Completed status.
		orderUpdate := &models.OrderUpdate{
			ID:              takerOrder.ID,
			Status:          models.OrderStatusCompleted,
			AvailableAmount: Zero,
			AvailableTotal:  Zero,
			ExecutedAmount:  result.AmountDone,
			CanceledAmount:  result.AmountLeft,
			CanceledTotal:   result.TotalLeft,
			ExecutedTotal:   result.TotalDone,
		}
		takerOrder.ApplyUpdate(orderUpdate)

	case models.OrderTypeLimit, models.OrderTypeStopLimit:
		// If the limit or stop limit order is not completely filled, we send it to the order book
		if !result.IsDone && result.AmountLeft.GreaterThan(Zero) {
			status := models.OrderStatusOpen
			if result.AmountDone.GreaterThan(Zero) {
				status = models.OrderStatusPartiallyCompleted
			}
			originalTakerOrder := *takerOrder
			orderUpdate := &models.OrderUpdate{
				ID:              takerOrder.ID,
				Status:          status,
				AvailableAmount: result.AmountLeft,
				ExecutedAmount:  result.AmountDone,
				CanceledAmount:  Zero,
				ExecutedTotal:   result.TotalDone,
			}
			takerOrder.ApplyUpdate(orderUpdate)
			err = book.InsertOrder(takerOrder)
			if err != nil {
				*takerOrder = originalTakerOrder

				return nil, fmt.Errorf("insert resting taker order: %w", err)
			}

			err = book.orders.UpdateOrder(ctx, takerOrder)
			if err != nil {
				rollbackErr := book.RemoveOrder(takerOrder)
				*takerOrder = originalTakerOrder
				if rollbackErr != nil {
					return nil, fmt.Errorf("update resting taker order: %w; rollback resting taker order: %v", err, rollbackErr)
				}

				return nil, fmt.Errorf("update resting taker order: %w", err)
			}
			response.InitialOrder = takerOrder
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
			response.InitialOrder = takerOrder
		}
	}

	for _, matchedOrder := range result.MatchedOrders {
		// filled out the order and removed it from the order book
		if matchedOrder.IsDone {
			originalMakerOrder := *matchedOrder.Order
			err = book.RemoveOrder(matchedOrder.Order)
			if err != nil {
				return nil, fmt.Errorf("remove completed maker order: %w", err)
			}
			orderUpdate := &models.OrderUpdate{
				ID:              matchedOrder.Order.ID,
				Status:          models.OrderStatusCompleted,
				AvailableAmount: Zero,
				ExecutedAmount:  matchedOrder.Order.ExecutedAmount.Add(matchedOrder.MatchedAmount),
				CanceledAmount:  Zero,
				ExecutedTotal:   matchedOrder.MatchedAmount.Mul(matchedOrder.Order.Price),
			}
			trade = &models.Trade{
				ID:           fmt.Sprintf("%s_%s", takerOrder.ID.String(), matchedOrder.Order.ID.String()),
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
				rollbackErr := book.InsertOrder(&originalMakerOrder)
				*matchedOrder.Order = originalMakerOrder
				if rollbackErr != nil {
					return nil, fmt.Errorf("update completed maker order: %w; rollback completed maker order: %v", err, rollbackErr)
				}

				return nil, fmt.Errorf("update completed maker order: %w", err)
			}

			if err = book.trades.ConsumeTrade(ctx, trade); err != nil {
				return nil, fmt.Errorf("consume trade: %w", err)
			}
			err = book.orderbookService.ConsumeTrade(ctx, trade)
			if err != nil {
				return nil, fmt.Errorf("consume orderbook trade: %w", err)
			}

			response.MatchedOrders = append(response.MatchedOrders, &models.MatchedOrderResult{
				Order: matchedOrder.Order,
				Trade: trade,
			})

			trades = append(trades, trade)
		} else {
			// Partially filled the maker order.
			originalMakerOrder := *matchedOrder.Order
			err = book.ChangeOrder(ctx, matchedOrder.Order, matchedOrder.MatchedAmount)
			if err != nil {
				return nil, fmt.Errorf("change partially matched maker order: %w", err)
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
				ID:           fmt.Sprintf("%s_%s", takerOrder.ID.String(), matchedOrder.Order.ID.String()),
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
				rollbackErr := book.ChangeOrder(ctx, &originalMakerOrder, matchedOrder.MatchedAmount.Neg())
				*matchedOrder.Order = originalMakerOrder
				if rollbackErr != nil {
					return nil, fmt.Errorf("update partially matched maker order: %w; rollback partially matched maker order: %v", err, rollbackErr)
				}

				return nil, fmt.Errorf("update partially matched maker order: %w", err)
			}

			response.MatchedOrders = append(response.MatchedOrders, &models.MatchedOrderResult{
				Order: matchedOrder.Order,
				Trade: trade,
			})

			if err = book.trades.ConsumeTrade(ctx, trade); err != nil {
				return nil, fmt.Errorf("consume trade: %w", err)
			}

			err := book.orderbookService.ConsumeTrade(ctx, trade)
			if err != nil {
				return nil, fmt.Errorf("consume orderbook trade: %w", err)
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
			return nil, fmt.Errorf("update taker order: %w", err)
		}

		response.InitialOrder = takerOrder

		market, err := book.markets.GetMarketByID(book.market.String())
		if err != nil {
			return nil, fmt.Errorf("get market: %w", err)
		}
		if market != nil {
			market.LastSpotPrice = lastPrice
			err = book.markets.UpdateMarket(market)
			if err != nil {
				return nil, fmt.Errorf("update market: %w", err)
			}
		}
	}

	return response, nil
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
	// remainder holds quote dust left on the last partial fill: the taker offered
	// leftTotal but the floored base amount only costs floored*price, so
	// leftTotal-floored*price cannot buy a whole base lot. It is returned to the
	// taker via TotalLeft rather than booked as executed.
	remainder := Zero

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
					matchedItem.MatchedAmount = book.divFloor(leftTotal, makerOrder.Price)
					matchedItem.IsDone = false
					matched := matchedItem.MatchedAmount.Mul(makerOrder.Price)
					matchedTotal = matchedTotal.Add(matched)
					remainder = leftTotal.Sub(matched)
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
					matchedItem.MatchedAmount = book.divFloor(leftTotal, makerOrder.Price)
					matchedItem.IsDone = false
					matched := matchedItem.MatchedAmount.Mul(makerOrder.Price)
					matchedTotal = matchedTotal.Add(matched)
					remainder = leftTotal.Sub(matched)
					leftTotal = Zero
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
		TotalLeft:     leftTotal.Add(remainder),
		TotalDone:     matchedTotal,
		AmountDone:    matchedAmount,
	}
}
