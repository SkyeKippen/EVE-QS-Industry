package main

import "testing"

func TestParsePrice(t *testing.T) {
	valid := map[string]float64{
		"40.34M":        40_340_000,
		"40.34m":        40_340_000,
		"1.5k":          1_500,
		"1.5K":          1_500,
		"8.5B":          8_500_000_000,
		"21b":           21_000_000_000,
		" 2 M ":         2_000_000,
		"8,138,285,000": 8_138_285_000,
		"1,250.5k":      1_250_500,
		"8500000000":    8_500_000_000,
		"0":             0,
		".5k":           500,
	}
	for in, want := range valid {
		got, err := parsePrice(in)
		if err != nil || got != want {
			t.Errorf("parsePrice(%q) = %v, %v; want %v", in, got, err, want)
		}
	}

	invalid := []string{"", "k", "abc", "-5M", "1.2345k", "1e9", "1/2", "1.2.3M", "5MB", "10000000000B"}
	for _, in := range invalid {
		if got, err := parsePrice(in); err == nil {
			t.Errorf("parsePrice(%q) = %v; want an error", in, got)
		}
	}
}
