package main

import "testing"

func TestTitleSim(t *testing.T) {
	for _, c := range []struct {
		a, b string
		ok   bool
	}{
		{"Oldboy", "Oldboy", true},
		{"A Clockwork Orange", "Clockwork Orange", true},
		{"True Detective", "True Detective", true},
		{"Mint Test", "Les Gorilles", false},
		{"The Office", "The Office (US)", true},
		{"Spider-Man: No Way Home", "Spider-Man No Way Home", true},
		{"Dune", "Dune: Part Two", false},
	} {
		if got := titleSim(c.a, c.b) >= 0.6; got != c.ok {
			t.Errorf("titleSim(%q, %q) = %.2f, want ok=%v", c.a, c.b, titleSim(c.a, c.b), c.ok)
		}
	}
}
