// Package engine is the order-matching core of the exchange. It routes orders
// to per-market handlers, maintains price-time-priority order books on
// left-leaning red-black trees, and matches taker orders against resting
// makers.
//
// # Concurrency model
//
// The engine uses a single-writer-per-market model. Each MarketHandler owns one
// Orderbook and is the only goroutine that mutates it: orders are serialized
// through the handler's incoming channel and applied one at a time in
// MarketHandler.Run. There is therefore no writer/writer contention on a book,
// and no lock is needed to make mutations safe against each other.
//
// The Orderbook's RWMutex exists for one purpose: to let a host take a
// consistent point-in-time Snapshot concurrently with the writer. Mutators take
// the write lock and Snapshot/MinAsk/MaxBid/GetOrder take the read lock.
//
// Match deliberately is not a single critical section. Holding the write lock
// across an entire match would also hold it across external persistence calls
// (OrderService, TradeService, OrderbookService), serializing the market and
// blocking Snapshot readers for the duration of that I/O. Instead each in-memory
// tree mutation locks individually, so a concurrent Snapshot may observe a match
// partway applied. That is acceptable for an advisory depth snapshot and is the
// reason the locking is per-operation rather than per-match.
//
// StopOrderListener is the exception: it runs in its own goroutine (started by
// MarketHandler.Run) and is genuinely concurrent with the handler — the handler
// inserts stop orders while the listener triggers and removes them on price
// updates — so it keeps its own RWMutex.
//
// # Public API surface
//
// The only supported entry point for host applications is Engine, constructed
// via NewEngine, together with the interfaces in package models that the host
// implements. Types such as Orderbook, PriceNode, MarketHandler and MatchResult
// are exported for white-box testing and advanced embedding; they are not part
// of the stability contract and may change without notice.
package engine
