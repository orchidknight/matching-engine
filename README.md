# matching-engine

`matching-engine` is an embeddable Go library for matching exchange orders. It is a core domain package, not a standalone service: the host application owns persistence, transport, authentication, market data, and event delivery.

The engine supports:

- price-time priority order books backed by left-leaning red-black trees;
- limit, market, stop-limit, and stop-market orders;
- amount-based orders and quote-total-based orders;
- self-trade prevention for orders from the same account;
- cold restart from persisted open and partially completed orders;
- explicit stop-order triggering through pushed market prices.

## Architecture

The runtime flow is intentionally small and host-driven:

```text
Host application
  ├─ implements MarketService / OrderService / TradeService / OrderbookService / Logger
  ├─ starts Engine.Run(ctx)
  ├─ submits orders with Engine.ConsumeOrder(order)
  ├─ submits prices with Engine.PushPrice(price)
  └─ drains responses with Engine.GetLastOrderResponse()

Engine
  └─ MarketHandler per symbol
       ├─ Orderbook
       │    ├─ bids LLRB tree
       │    └─ asks LLRB tree
       └─ StopOrderListener
```

- `Engine` routes incoming orders and price updates by `models.Symbol`.
- `MarketHandler` processes one market sequentially, so each market has a single writer for matching state.
- `Orderbook` stores bids and asks as red-black trees of price levels.
- `StopOrderListener` tracks stop orders and re-submits triggered orders into the market handler.

## Matching model

The order book uses price-time priority:

- price priority comes from the LLRB tree ordering of price levels;
- time priority comes from FIFO insertion order inside each price level;
- buy takers walk asks from lowest to highest price;
- sell takers walk bids from highest to lowest price;
- limit orders stop matching when the next price level violates the taker's limit price;
- orders from the same account are skipped to prevent self-trades.

## Host interfaces

The library integrates with the host through interfaces in `models/interfaces.go`.

| Interface | Host responsibility |
|---|---|
| `MarketService` | Provide markets on startup and update last traded price |
| `OrderService` | Persist order updates, rejects, cancellations, and startup recovery |
| `TradeService` | Consume trades and provide last price for stop-order activation |
| `OrderbookService` | Consume orderbook trade events, for example for websocket streams |
| `Logger` | Receive structured component logs |

`NewEngine` calls `MarketService.GetMarkets()` and then `OrderService.GetOrdersByPair()` for each market. Open, partially completed, and pending stop orders are restored into memory before `Run` starts processing new input.

## Basic usage

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

matchingEngine, err := engine.NewEngine(
	ctx,
	marketService,
	orderService,
	tradeService,
	orderbookService,
	logger,
)
if err != nil {
	return err
}

go func() {
	if err := matchingEngine.Run(ctx); err != nil {
		logger.Error("engine", "run: %v", err)
	}
}()

matchingEngine.ConsumeOrder(&models.Order{
	ID:              uuid.New(),
	Account:         "taker",
	Symbol:          "BTC-USDT",
	Type:            models.OrderTypeLimit,
	Status:          models.OrderStatusNew,
	Side:            models.Buy,
	AvailableAmount: decimal.NewFromInt(1),
	Price:           decimal.NewFromInt(100),
})

response := matchingEngine.GetLastOrderResponse()
fmt.Println(response.InitialOrder.Status)
```

See `engine/example_test.go` for a complete executable example with in-memory mock implementations. It is compiled and checked by `go test ./...`.

## Stop order price feed

Stop orders are triggered by explicit price updates from the host application. After starting the engine, push the latest market price:

```go
matchingEngine.PushPrice(models.Price{
	Symbol:    "BTC-USDT",
	LastPrice: decimal.NewFromInt(100),
})
```

The engine routes the update to the matching market's `StopOrderListener`. Triggered stop orders are pushed back into the same market handler as regular orders.

## Order response drainage

The host application must continuously drain order responses with `Engine.GetLastOrderResponse()`. The response channel is buffered, but it is still backpressure: if the buffer is full and the engine context is not canceled, market handlers wait until the host reads responses.

When the engine context is canceled, response sends unblock and market goroutines can shut down cleanly.

## Validation and precision

`MarketHandler` validates incoming trading orders before stop handling, matching, or book insertion:

- minimum order size is checked against `AvailableAmount`, or `AvailableTotal` when amount is not set;
- limit prices and stop activation prices must fit quote-asset input precision;
- base amounts must fit base-asset input precision;
- quote totals must fit quote-asset input precision;
- accepted amount, total, price, and activation price fields are rounded to asset calculation precision.

## Project status

This repository is a portfolio-quality matching-engine core. It intentionally excludes service concerns such as HTTP/gRPC APIs, databases, authentication, message brokers, deployments, and wallet/accounting integration.

Current scope:

- Go module: `github.com/orchidknight/matching-engine`
- Go version: `1.24.5`
- Package layout: `models` for domain types and host interfaces, `engine` for matching logic
- Test focus: core matching behavior, orderbook behavior, stop-order triggering, and runnable examples

Run the suite with:

```sh
go test ./...
```
