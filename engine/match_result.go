package engine

import (
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/orchidknight/matching-engine/models"
)

type MatchResult struct {
	Order         *models.Order          `json:"order"`         // taker order
	MatchedOrders []*models.MatchedOrder `json:"matchedOrders"` // maker orders

	IsDone     bool            `json:"isDone"`
	AmountLeft decimal.Decimal `json:"amountLeft"`
	TotalLeft  decimal.Decimal `json:"totalLeft"`
	TotalDone  decimal.Decimal `json:"totalDone"`
	AmountDone decimal.Decimal `json:"amountDone"`
	Error      error           `json:"error"`
}

func (mr *MatchResult) String() string {
	var matches string
	for _, match := range mr.MatchedOrders {
		matches += fmt.Sprintf(" %v-%v,", match.Order.ID, match.MatchedAmount)
	}

	return fmt.Sprintf("%s to [%s] isDone: %t AmountLeft:%v TotalDone:%v, AmountDone:%v", mr.Order.ID, matches, mr.IsDone, mr.AmountLeft, mr.TotalDone, mr.AmountDone)
}
