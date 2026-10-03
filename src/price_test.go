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
		"4.5":           4.5,
		"4.25":          4.25,
		"0.01":          0.01,
		"1.2345k":       1234.5,
		"1.23456k":      1234.56,
	}
	for in, want := range valid {
		got, err := parsePrice(in)
		if err != nil || got != want {
			t.Errorf("parsePrice(%q) = %v, %v; want %v", in, got, err, want)
		}
	}

	invalid := []string{"", "k", "abc", "-5M", "4.255", "1.234567k", "1e9", "1/2", "1.2.3M", "5MB", "10000000000B"}
	for _, in := range invalid {
		if got, err := parsePrice(in); err == nil {
			t.Errorf("parsePrice(%q) = %v; want an error", in, got)
		}
	}
}

func TestOrderTotal(t *testing.T) {
	cases := []struct {
		unit, total, basis string
		qty                int64
		want               float64
	}{
		{"3.4", "340", "unit", 100, 340},
		{"3.4", "", "total", 100, 340},
		{"", "340", "unit", 100, 340},
		{"3.4", "350", "total", 100, 350},
		{"3.4", "350", "unit", 100, 340},
		{"1.5k", "", "", 3, 4500},
		{"0.07", "", "unit", 3, 0.21},
		{"33.33", "100", "", 3, 100},
	}
	for _, c := range cases {
		got, err := orderTotal(c.unit, c.total, c.basis, c.qty)
		if err != nil || got != c.want {
			t.Errorf("orderTotal(%q, %q, %q, %d) = %v, %v; want %v", c.unit, c.total, c.basis, c.qty, got, err, c.want)
		}
	}

	invalid := []struct {
		unit, total, basis string
		qty                int64
	}{
		{"", "", "total", 1},
		{"abc", "", "unit", 1},
		{"3.4", "abc", "total", 1},
		{"9B", "", "unit", 1_000_000},
	}
	for _, c := range invalid {
		if got, err := orderTotal(c.unit, c.total, c.basis, c.qty); err == nil {
			t.Errorf("orderTotal(%q, %q, %q, %d) = %v; want an error", c.unit, c.total, c.basis, c.qty, got)
		}
	}
}

func TestParseQuantity(t *testing.T) {
	valid := map[string]int64{
		"100":      100,
		"1,200":    1_200,
		"1.2k":     1_200,
		"1.2K":     1_200,
		"3M":       3_000_000,
		"2b":       2_000_000_000,
		" 12,500 ": 12_500,
	}
	for in, want := range valid {
		got, err := parseQuantity(in)
		if err != nil || got != want {
			t.Errorf("parseQuantity(%q) = %v, %v; want %v", in, got, err, want)
		}
	}

	invalid := []string{"", "0", "1.5", "1.2345k", "-5", "abc", "k", "1e3", "10000000000B"}
	for _, in := range invalid {
		if got, err := parseQuantity(in); err == nil {
			t.Errorf("parseQuantity(%q) = %v; want an error", in, got)
		}
	}
}
