package main

// Typo-tolerant matching, used as a fallback when a search term has no exact
// substring match. A term matches a word if the (Damerau-Levenshtein) edit
// distance is within a budget based on term length -- the same shape of rule
// Meilisearch uses: short terms must be exact, medium terms tolerate one
// typo, long terms two.

func maxEdits(termLen int) int {
	switch {
	case termLen < 4:
		return 0
	case termLen < 8:
		return 1
	default:
		return 2
	}
}

// editDistance returns the optimal-string-alignment distance between a and b
// (insert/delete/substitute/transpose), or bound+1 as soon as it can prove
// the distance exceeds bound.
func editDistance(a, b []rune, bound int) int {
	la, lb := len(a), len(b)
	if la-lb > bound || lb-la > bound {
		return bound + 1
	}
	prev2 := make([]int, lb+1)
	prev := make([]int, lb+1)
	cur := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		cur[0] = i
		rowMin := cur[0]
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			d := min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				d = min(d, prev2[j-2]+1)
			}
			cur[j] = d
			rowMin = min(rowMin, d)
		}
		if rowMin > bound {
			return bound + 1
		}
		prev2, prev, cur = prev, cur, prev2
	}
	return prev[lb]
}

// fuzzyMatch reports whether term matches any word within the edit budget.
// Longer words also match on their prefix, so a partially typed (and
// mistyped) term like "resistr" still hits "resistors".
func fuzzyMatch(term string, words [][]rune) bool {
	t := []rune(term)
	bound := maxEdits(len(t))
	if bound == 0 {
		return false
	}
	for _, w := range words {
		if len(w) < len(t)-bound {
			continue
		}
		if len(w) > len(t) {
			if editDistance(t, w[:min(len(w), len(t))], bound) <= bound {
				return true
			}
		}
		if editDistance(t, w, bound) <= bound {
			return true
		}
	}
	return false
}
