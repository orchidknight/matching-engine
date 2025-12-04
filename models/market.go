package models

import (
	"github.com/shopspring/decimal"
)

type Asset struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	Address              string `json:"address"`
	CalculationPrecision int    `json:"calculationPrecision"`
	InputPrecision       int    `json:"inputPrecision"`
	IsPublished          bool   `json:"isPublished"`
}

type Market struct {
	ID            Symbol          `json:"id"`
	BaseAsset     *Asset          `json:"baseToken"`
	QuoteAsset    *Asset          `json:"quoteToken"`
	MinOrderSize  decimal.Decimal `json:"minOrderSize"`
	TakerFee      decimal.Decimal `json:"takerFee"`
	MakerFee      decimal.Decimal `json:"makerFee"`
	IsPublished   bool            `json:"isPublished"`
	LastSpotPrice decimal.Decimal `json:"lastSpotPrice"`
}
