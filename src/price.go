package main

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
)

// priceSuffixes maps a shorthand suffix to its multiplier. "k" is accepted in
// either case; "M" and "B" are also accepted lowercase since "m" and "b" have
// no other meaning in a price.
var priceSuffixes = map[byte]int64{
	'k': 1_000,
	'm': 1_000_000,
	'b': 1_000_000_000,
}

// parseShorthand turns user-entered numbers such as "40.34M", "1.5k", "8.5B"
// or "8,138,285,000" into an exact decimal. Thousands separators are ignored.
// what names the field in error messages.
func parseShorthand(input, what string) (*big.Rat, error) {
	s := strings.ReplaceAll(strings.TrimSpace(input), ",", "")
	if s == "" {
		return nil, errors.New(what + " is empty")
	}

	multiplier := int64(1)
	if m, ok := priceSuffixes[strings.ToLower(s[len(s)-1:])[0]]; ok {
		multiplier = m
		s = strings.TrimSpace(s[:len(s)-1])
	}

	// big.Rat accepts fractions and exponents, so only allow digits and one dot.
	if s == "" || strings.Trim(s, "0123456789.") != "" || strings.Count(s, ".") > 1 || s == "." {
		return nil, errors.New(what + " is not a number")
	}

	value, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil, errors.New(what + " is not a number")
	}
	value.Mul(value, new(big.Rat).SetInt64(multiplier))

	if value.Cmp(new(big.Rat).SetInt64(math.MaxInt64)) >= 0 {
		return nil, errors.New(what + " is too large")
	}
	return value, nil
}

// parsePrice turns user-entered ISK such as "40.34M" into ISK. The math is
// done on exact decimals so "40.34M" is 40340000, not 40339999.999. It errors
// on anything negative or with more than 2 decimal places of ISK (after the
// suffix is applied, so "1.2345k" is 1234.50 and allowed).
func parsePrice(input string) (float64, error) {
	value, err := parseShorthand(input, "price")
	if err != nil {
		return 0, err
	}
	if !new(big.Rat).Mul(value, big.NewRat(100, 1)).IsInt() {
		return 0, errors.New("price can have at most 2 decimal places of ISK")
	}
	price, _ := value.Float64()
	return price, nil
}

// parseQuantity turns a user-entered quantity such as "1.2k", "3M" or
// "12,500" into a whole number greater than zero.
func parseQuantity(input string) (int64, error) {
	value, err := parseShorthand(input, "quantity")
	if err != nil {
		return 0, err
	}
	if !value.IsInt() || value.Sign() <= 0 {
		return 0, errors.New("quantity must be a whole number greater than zero")
	}
	return value.Num().Int64(), nil
}

// maxOrderTotal keeps a computed total inside the order_price numeric(20,2)
// column and the range where float64 still holds every cent exactly.
const maxOrderTotal = 1e15

// orderTotal works out the ISK total to store for an order from the
// "price per item" and "price total" fields. The two fields can disagree
// once a pilot edits an autofilled value, so basis names the one they typed
// in last: "unit" stores unit times quantity, anything else stores the total as
// entered. Whichever field basis names may be left blank, in which case the
// other one is used.
func orderTotal(unitInput, totalInput, basis string, quantity int64) (float64, error) {
	useUnit := basis == "unit"
	if strings.TrimSpace(unitInput) == "" {
		useUnit = false
	} else if strings.TrimSpace(totalInput) == "" {
		useUnit = true
	}

	if !useUnit {
		total, err := parsePrice(totalInput)
		if err != nil {
			return 0, fmt.Errorf("price total: %w", err)
		}
		return total, nil
	}

	unit, err := parsePrice(unitInput)
	if err != nil {
		return 0, fmt.Errorf("price per item: %w", err)
	}
	if unit*float64(quantity) >= maxOrderTotal {
		return 0, errors.New("price total is too large")
	}
	// The unit price has at most 2 decimals, so multiply whole cents.
	return math.Round(unit*100) * float64(quantity) / 100, nil
}
