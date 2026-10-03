package main

import (
	"errors"
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

// parsePrice turns user-entered ISK such as "40.34M", "1.5k", "8.5B" or
// "8,138,285,000" into ISK. Thousands separators are ignored. The math is done
// on exact decimals so "40.34M" is 40340000, not 40339999.999. It errors on
// anything negative or with more than 2 decimal places of ISK (after the
// suffix is applied, so "1.2345k" is 1234.50 and allowed).
func parsePrice(input string) (float64, error) {
	s := strings.ReplaceAll(strings.TrimSpace(input), ",", "")
	if s == "" {
		return 0, errors.New("price is empty")
	}

	multiplier := int64(1)
	if m, ok := priceSuffixes[strings.ToLower(s[len(s)-1:])[0]]; ok {
		multiplier = m
		s = strings.TrimSpace(s[:len(s)-1])
	}

	// big.Rat accepts fractions and exponents, so only allow digits and one dot.
	if s == "" || strings.Trim(s, "0123456789.") != "" || strings.Count(s, ".") > 1 || s == "." {
		return 0, errors.New("price is not a number")
	}

	value, ok := new(big.Rat).SetString(s)
	if !ok {
		return 0, errors.New("price is not a number")
	}
	value.Mul(value, new(big.Rat).SetInt64(multiplier))

	if !new(big.Rat).Mul(value, big.NewRat(100, 1)).IsInt() {
		return 0, errors.New("price can have at most 2 decimal places of ISK")
	}
	if value.Cmp(new(big.Rat).SetInt64(math.MaxInt64)) >= 0 {
		return 0, errors.New("price is too large")
	}
	price, _ := value.Float64()
	return price, nil
}
