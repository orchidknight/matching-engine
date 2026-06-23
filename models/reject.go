package models

type RejectReason string

const (
	RejectReasonUnspecified            = "Unspecified"
	RejectReasonNotEnoughLiquidity     = "Not enough liquidity"
	RejectReasonNoMatches              = "Order got zero matches"
	RejectReasonWrongSymbol            = "Order has wrong symbol"
	RejectReasonWrongInput             = "Order has wrong original values"
	RejectReasonMinOrderSize           = "Order size is below market minimum"
	RejectReasonInvalidPricePrecision  = "Order price precision exceeds market tick size"
	RejectReasonInvalidAmountPrecision = "Order amount precision exceeds market lot size"
	RejectReasonInvalidTotalPrecision  = "Order total precision exceeds quote precision"
)
