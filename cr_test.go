package character

import "testing"

func TestParseCR(t *testing.T) {
	good := map[string]float64{"0": 0, "1/8": 0.125, "1/4": 0.25, "1/2": 0.5, "1": 1, "30": 30}
	for s, want := range good {
		if got, ok := parseCR(s); !ok || got != want {
			t.Errorf("parseCR(%q) = %v, %v; want %v, true", s, got, ok, want)
		}
	}
	for _, s := range []string{"", "1/3", "31", "-1", "x"} {
		if _, ok := parseCR(s); ok {
			t.Errorf("parseCR(%q) should not be ok", s)
		}
	}
}

func TestFormatCR(t *testing.T) {
	for v, want := range map[float64]string{0.25: "1/4", 3: "3", 0.5: "1/2", 0.125: "1/8", 0: "0"} {
		if got := formatCR(v); got != want {
			t.Errorf("formatCR(%v) = %q, want %q", v, got, want)
		}
	}
}
