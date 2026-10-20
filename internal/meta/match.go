package meta

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/width"
)

// trialRe matches the edition words that distribution titles carry but database titles do not.
var trialRe = regexp.MustCompile(`(?i)((ダウンロード|web|ウェブ)\s*)?(体験|お試し読み|お試し|プレイアブル)\s*版|(ダウンロード|web|ウェブ)\s*版|trial(\s*version|\s*edition)?|demo|taiken|ver\.?\s*[0-9.]+|[-－‐]?\s*(web|dl)\s*$`)

// SearchKey turns a package title into a keyword for a substring search.
func SearchKey(title string) string {
	s := width.Fold.String(title)
	s = trialRe.ReplaceAllString(s, " ")
	s = strings.Trim(strings.Join(strings.Fields(s), " "), " -_~〜～()（）[]【】「」")
	return s
}

var segRe = regexp.MustCompile(`[\s~〜～・:：!！?？()（）\[\]【】「」『』]+`)

// SearchKeys returns the keyword to try first and, when the title has symbols whose
// spelling differs between databases (～ vs ~ ...), its longest symbol-free part as a retry.
func SearchKeys(title string) []string {
	k := SearchKey(title)
	keys := []string{k}
	longest := ""
	for _, p := range segRe.Split(k, -1) {
		if len([]rune(p)) > len([]rune(longest)) {
			longest = p
		}
	}
	if longest != k && len([]rune(longest)) >= 3 {
		keys = append(keys, longest)
	}
	return keys
}

// norm folds width and case and drops edition words, spaces and symbols.
func norm(s string) string {
	s = strings.ToLower(width.Fold.String(s))
	s = trialRe.ReplaceAllString(s, "")
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || r == 'ー' {
			return r
		}
		return -1
	}, s)
}

// Best picks the candidate matching title: an exact match after normalisation, else the
// closest one that contains (or is contained in) the title; ties go to the most voted entry.
func Best(title string, cands []Candidate) (Candidate, bool) {
	q := norm(title)
	if q == "" {
		return Candidate{}, false
	}
	var best Candidate
	bestScore := -1 << 30
	for _, c := range cands {
		n := norm(c.Title)
		if n == "" {
			continue
		}
		score := 0
		switch {
		case n == q:
			score = 1 << 20
		case strings.Contains(n, q) || strings.Contains(q, n):
			score = 1<<19 - abs(len([]rune(n))-len([]rune(q)))*1000
		default:
			continue
		}
		score += min(c.Votes, 999)
		if score > bestScore {
			best, bestScore = c, score
		}
	}
	return best, bestScore > -1<<30
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
