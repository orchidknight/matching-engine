# matching-engine
A matching engine used as the core for processing orders on the exchange. The order book is built on red-black trees. It supports orders based on the underlying asset's volume and orders based on the entire volume. It also supports market orders and limit orders. A module for processing stop orders is also available.

## Stop order price feed

Stop orders are triggered by explicit price updates from the host application. After starting the engine, push the latest market price with `Engine.PushPrice(models.Price{Symbol: symbol, LastPrice: price})`; the engine routes the update to the matching market's stop-order listener.

## Order response drainage

The host application must continuously drain order responses with `Engine.GetLastOrderResponse()`. The response channel is buffered, but it is still backpressure: if the buffer is full and the engine context is not canceled, market handlers wait until the host reads responses. When the engine context is canceled, response sends unblock and market goroutines can shut down cleanly.
