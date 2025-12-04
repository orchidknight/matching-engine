package models

type RejectReason string

const (
	RejectReasonUnspecified        = "Unspecified"
	RejectReasonNotEnoughLiquidity = "Not enough liquidity"
	RejectReasonNoMatches          = "Order got zero matches"
)
