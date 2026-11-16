package character

import (
	"strconv"
	"strings"
)

// parseCR reads a challenge rating: "0", "1/8", "1/4", "1/2", or a whole number 1 to 30.
func parseCR(s string) (float64, bool) {
	switch s {
	case "0":
		return 0, true
	case "1/8":
		return 0.125, true
	case "1/4":
		return 0.25, true
	case "1/2":
		return 0.5, true
	}
	if s == "" || strings.HasPrefix(s, "+") {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 30 {
		return 0, false
	}
	return float64(n), true
}

// formatCR is the inverse of parseCR.
func formatCR(v float64) string {
	switch v {
	case 0.125:
		return "1/8"
	case 0.25:
		return "1/4"
	case 0.5:
		return "1/2"
	}
	return strconv.Itoa(int(v))
}
