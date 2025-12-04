package models

import (
	"fmt"
	"strings"
)

type Symbol string

var assetSeparator = "-"

func (s Symbol) String() string {
	return string(s)
}

func NewSymbol(s string) (Symbol, error) {
	parts := strings.Split(s, assetSeparator)
	if len(parts) != 2 {
		return "", fmt.Errorf("wrong symbol format %s", s)
	}

	return Symbol(s), nil
}
