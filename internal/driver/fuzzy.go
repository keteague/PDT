package driver

import "strings"

// FuzzyMatchScore ports Get-FuzzyMatchScore: higher is a better match, -1
// means no match at all. A substring match scores best (shorter matched text
// wins ties via -len); otherwise an in-order subsequence match scores lower,
// same tie-break. Matching is byte-wise (ASCII) since driver/manufacturer
// names in practice never contain multi-byte characters, mirroring the
// original's plain char-by-char IndexOf loop.
func FuzzyMatchScore(text, query string) int {
	if query == "" {
		return 0
	}
	t := strings.ToLower(text)
	q := strings.ToLower(query)
	if strings.Contains(t, q) {
		return 1000 - len(t)
	}

	ti := 0
	for i := 0; i < len(q); i++ {
		idx := strings.IndexByte(t[ti:], q[i])
		if idx < 0 {
			return -1
		}
		ti += idx + 1
	}
	return 100 - len(t)
}
