package engine

import (
	"context"
	"fmt"
	"math"
	"sync"

	"github.com/petar/GoLLRB/llrb"
	"github.com/shopspring/decimal"

	"github.com/orchidknight/matching-engine/models"
)

type Orderbook struct {
	market           models.Symbol
	orders           models.OrderService
	markets          models.MarketService
	trades           models.TradeService
	orderbookService models.OrderbookService

	bidsTree   *llrb.LLRB
	bidsVolume decimal.Decimal
	asksTree   *llrb.LLRB
	asksVolume decimal.Decimal

	lock sync.RWMutex
	log  models.Logger
}

// NewOrderbook return a new book
func NewOrderbook(market models.Symbol, service models.OrderbookService, o models.OrderService, m models.MarketService, t models.TradeService, log models.Logger) *Orderbook {
	book := &Orderbook{
		market:           market,
		bidsTree:         llrb.New(),
		asksTree:         llrb.New(),
		lock:             sync.RWMutex{},
		orderbookService: service,
		log:              log,
		asksVolume:       decimal.NewFromUint64(0),
		bidsVolume:       decimal.NewFromUint64(0),
		orders:           o,
		markets:          m,
		trades:           t,
	}

	return book
}

func (book *Orderbook) InsertOrder(order *models.Order) error {
	var err error
	book.lock.Lock()
	defer book.lock.Unlock()
	var tree *llrb.LLRB

	switch order.Side {
	case models.Sell:
		tree = book.asksTree
		price := tree.Get(newPriceNode(order.Price))
		if price == nil {
			price = newPriceNode(order.Price)
			tree.InsertNoReplace(price)
		}
		err = price.(*PriceNode).InsertOrder(*order)
		if err != nil {
			return err
		}

	case models.Buy:
		tree = book.bidsTree
		price := tree.Get(newPriceNode(order.Price))
		if price == nil {
			price = newPriceNode(order.Price)
			tree.InsertNoReplace(price)
		}
		err = price.(*PriceNode).InsertOrder(*order)
		if err != nil {
			return err
		}
	}

	book.log.Debug("book", "Inserted: %d P:%s A:%s", order.ID, order.Price.String(), order.Amount.String())

	return nil
}

func (book *Orderbook) ChangeOrder(_ context.Context, order *models.Order, changeAmount decimal.Decimal) error {
	var err error
	book.lock.Lock()
	defer book.lock.Unlock()

	var tree *llrb.LLRB
	if order.Side == models.Sell {
		tree = book.asksTree
	} else {
		tree = book.bidsTree
	}

	node := tree.Get(newPriceNode(order.Price))

	if node == nil {
		return fmt.Errorf("can't change order which is not in this orderbookService. book: %s, order: %+v", book.market, order)
	}
	price := node.(*PriceNode)
	err = price.UpdateOrder(order.ID, changeAmount)
	if err != nil {
		return err
	}
	if price.Len() <= 0 {
		tree.Delete(price)
	}
	if price.totalAmount.LessThanOrEqual(decimal.NewFromUint64(0)) {
		tree.Delete(price)
	}

	book.log.Debug("book", "Change order <%v>: Available amount decreases on %v", order, changeAmount)

	return nil
}

// RemoveOrder removes order from orderbook tree
func (book *Orderbook) RemoveOrder(order *models.Order) error {
	var err error
	book.lock.Lock()
	defer book.lock.Unlock()

	var tree *llrb.LLRB
	if order.Side == models.Sell {
		tree = book.asksTree
	}
	if order.Side == models.Buy {
		tree = book.bidsTree
	}

	plItem := tree.Get(newPriceNode(order.Price))
	if plItem == nil {
		return fmt.Errorf("can't remove order which is not in orderbookService")
	}

	price := plItem.(*PriceNode)

	if price == nil {
		return fmt.Errorf("pl is nil when RemoveOrder, book: %s, order: %+v", book.market, order)
	}

	err = price.RemoveOrder(order.ID)
	if err != nil {
		fmt.Printf("Error remove orderiD: %v", order.ID)

		return err
	}

	if price.Len() <= 0 {
		tree.Delete(price)
	}
	if price.totalAmount.LessThanOrEqual(decimal.NewFromUint64(0)) {
		tree.Delete(price)
	}

	return nil
}

func (book *Orderbook) Snapshot() models.Snapshot {
	bids := make([][2]decimal.Decimal, 0)
	asks := make([][2]decimal.Decimal, 0)

	asyncWaitGroup := sync.WaitGroup{}

	asyncWaitGroup.Add(1)
	go func() {
		var asksVolume decimal.Decimal
		book.asksTree.AscendGreaterOrEqual(newPriceNode(decimal.NewFromUint64(0)), func(i llrb.Item) bool {
			pl := i.(*PriceNode)
			if pl.price.Equal(decimal.NewFromUint64(0)) {
				return true
			}
			asks = append(asks, [2]decimal.Decimal{pl.price, pl.totalAmount})
			asksVolume = asksVolume.Add(pl.totalAmount)

			return true
		})
		book.asksVolume = asksVolume
		asyncWaitGroup.Done()
	}()

	asyncWaitGroup.Add(1)
	go func() {
		var bidsVolume decimal.Decimal
		book.bidsTree.DescendLessOrEqual(newPriceNode(decimal.NewFromUint64(math.MaxUint64)), func(i llrb.Item) bool {
			pl := i.(*PriceNode)
			if pl.price.Equal(decimal.NewFromUint64(0)) {
				return true
			}
			bids = append(bids, [2]decimal.Decimal{pl.price, pl.totalAmount})
			bidsVolume = bidsVolume.Add(pl.totalAmount)

			return true
		})
		book.bidsVolume = bidsVolume
		asyncWaitGroup.Done()
	}()

	asyncWaitGroup.Wait()

	res := models.Snapshot{
		Symbol: book.market,
		Bids:   bids,
		Asks:   asks,
	}

	return res
}

// CanMatchImmediately indicates if order can match IMMEDIATELY
// true if it is MARKET order
// true if it is LIMIT order and its price is completely fits to nearest ask minimum or bid maximum because it means
// that this limit order is the same as market order
func (book *Orderbook) CanMatchImmediately(order *models.Order) bool {
	if order.Type == models.OrderTypeMarket || order.Type == models.OrderTypeStopMarket {
		return true
	}

	if order.Side == models.Buy {
		minItem := book.asksTree.Min()
		if minItem == nil {
			return false
		}

		if order.Price.GreaterThanOrEqual(minItem.(*PriceNode).price) {
			return true
		}

		return false
	}
	if order.Side == models.Sell {
		maxItem := book.bidsTree.Max()
		if maxItem == nil {
			return false
		}

		if order.Price.LessThanOrEqual(maxItem.(*PriceNode).price) {
			return true
		}

		return false
	}

	return true
}

func (book *Orderbook) EnoughLiquidity(order *models.Order) bool {
	if order.Type != models.OrderTypeMarket {
		return true
	}

	switch order.Side {
	case models.Sell:
		if book.bidsVolume.Sub(order.AvailableAmount).GreaterThanOrEqual(Zero) {
			return true
		}
	case models.Buy:
		if book.asksVolume.Sub(order.AvailableAmount).GreaterThanOrEqual(Zero) {
			return true
		}
	}

	return false
}

func (book *Orderbook) MinAsk() decimal.Decimal {
	book.lock.Lock()
	defer book.lock.Unlock()

	minItem := book.asksTree.Min()

	if minItem != nil {
		return minItem.(*PriceNode).price
	}

	return decimal.NewFromUint64(0)
}

func (book *Orderbook) GetOrder(id uint64, side string, price decimal.Decimal) (models.Order, bool) {
	book.lock.Lock()
	defer book.lock.Unlock()

	var tree *llrb.LLRB
	if side == "sell" {
		tree = book.asksTree
	} else {
		tree = book.bidsTree
	}

	pl := tree.Get(newPriceNode(price))

	if pl == nil {
		return models.Order{}, false
	}

	return pl.(*PriceNode).GetOrder(id)
}

func (book *Orderbook) MaxBid() decimal.Decimal {
	book.lock.Lock()
	defer book.lock.Unlock()

	maxItem := book.bidsTree.Max()
	if maxItem != nil {
		return maxItem.(*PriceNode).price
	}

	return decimal.NewFromUint64(0)
}
