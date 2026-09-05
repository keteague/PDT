package config

import "testing"

func TestParseCsvBool(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"   ", false},
		{"0", false},
		{"false", false},
		{"FALSE", false},
		{"No", false},
		{"n", false},
		{"off", false},
		{"OFF", false},
		{"1", true},
		{"true", true},
		{"yes", true},
		{"y", true},
		{"on", true},
		{"anything else", true},
	}
	for _, c := range cases {
		if got := ParseCsvBool(c.in); got != c.want {
			t.Errorf("ParseCsvBool(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
