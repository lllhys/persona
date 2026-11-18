package main

import (
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// normalize keeps letters and digits after NFKC and lower case: punctuation, spaces and symbols never count as errors.
func normalize(s string) string {
	s = norm.NFKC.String(strings.ToLower(s))
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// cer is the character error rate of hyp against ref: the edit distance of their normalized texts over ref's length.
func cer(ref, hyp string) float64 {
	a, b := []rune(normalize(ref)), []rune(normalize(hyp))
	if len(a) == 0 {
		if len(b) == 0 {
			return 0
		}
		return 1
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			c := 1
			if a[i-1] == b[j-1] {
				c = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+c)
		}
		prev, cur = cur, prev
	}
	return float64(prev[len(b)]) / float64(len(a))
}

// latinWords are the words in Latin script of a text, lower case, two letters or more.
func latinWords(s string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(norm.NFKC.String(s)), func(r rune) bool {
		return !(r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)))
	}) {
		if len(w) >= 2 {
			out = append(out, w)
		}
	}
	return out
}

// latinRecall is the share of the reference lines' Latin words found, still in Latin script, in hyp: whether English
// said inside Chinese (and English lines) came out as said rather than translated.
func latinRecall(refs []string, hyp string) float64 {
	h := strings.ToLower(norm.NFKC.String(hyp))
	n, found := 0, 0
	for _, r := range refs {
		for _, w := range latinWords(r) {
			n++
			if strings.Contains(h, w) {
				found++
			}
		}
	}
	if n == 0 {
		return 0
	}
	return float64(found) / float64(n)
}

// hanShare is the share of Han characters among the Han and Latin letters of a text: near 1 for Chinese prose with a
// few names, near 0 for English.
func hanShare(s string) float64 {
	han, latin := 0, 0
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Han, r):
			han++
		case r < 128 && unicode.IsLetter(r):
			latin++
		}
	}
	if han+latin == 0 {
		return 0
	}
	return float64(han) / float64(han+latin)
}

// summary is n, min, p50, p90 and max of a sample; nil for none.
func summary(v []float64) map[string]float64 {
	if len(v) == 0 {
		return nil
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	q := func(p float64) float64 { return s[min(len(s)-1, int(p*float64(len(s))))] }
	return map[string]float64{"n": float64(len(s)), "min": s[0], "p50": q(0.5), "p90": q(0.9), "max": s[len(s)-1]}
}
