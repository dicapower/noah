// Package query turns a natural-language question into a safe FTS5 query.
//
// Agents search with questions ("how did we stop duplicated invoices?"), not
// keywords. Requiring every word to match returns nothing, so this builds an
// OR query over the meaningful words and lets BM25 rank by how many match and
// how rare they are. Long words are matched by prefix so that simple
// inflections (despliegue/despliegues, retry/retried) still hit.
package query

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

var stop = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`
a about after again all also am an and any are as at be because been before being but by can could did do does doing done for from
had has have having he her here him his how i if in into is it its just me more most my no nor not of on once only or other our out
over own same she should so some such than that the their them then there these they this those through to too under until up very
was we were what when where which while who whom why will with would you your
de la el los las un una unos unas y o u en con por para que como cual cuales quien donde cuando cuanto es son fue fueron ser estar
esta este estos estas se su sus al del lo le les me mi mis nos nuestro nuestra tu tus yo ya muy mas pero si no sin sobre entre hay
ha han he hemos habia hace hizo hicimos hicieron cuál qué quién dónde cómo cuándo`) {
		stop[w] = true
		stop[fold(w)] = true
	}
}

// fold lowercases and strips diacritics.
func fold(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, err := transform.String(t, strings.ToLower(s))
	if err != nil {
		return strings.ToLower(s)
	}
	return out
}

// Terms returns the meaningful, de-duplicated words of q in order.
func Terms(q string) []string {
	var words []string
	cur := strings.Builder{}
	flush := func() {
		if cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
	}
	for _, r := range q {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()

	seen := map[string]bool{}
	var keep []string
	for _, w := range words {
		f := fold(w)
		if len([]rune(f)) < 2 || stop[f] || seen[f] {
			continue
		}
		seen[f] = true
		keep = append(keep, f)
	}
	// If everything was a stopword (e.g. "how to"), fall back to the raw words.
	if len(keep) == 0 {
		for _, w := range words {
			f := fold(w)
			if len([]rune(f)) >= 2 && !seen[f] {
				seen[f] = true
				keep = append(keep, f)
			}
		}
	}
	return keep
}

// stem trims a word to a prefix so simple inflections match. It is a
// heuristic, not a linguistic stemmer: short words are kept whole.
func stem(w string) string {
	r := []rune(w)
	switch {
	case len(r) >= 9:
		return string(r[:len(r)-3])
	case len(r) >= 7:
		return string(r[:len(r)-2])
	case len(r) >= 5:
		return string(r[:len(r)-1])
	}
	return w
}

// Match builds an FTS5 query. mode "all" requires every term, "any" (default)
// matches any term. It returns "" when q has no usable terms.
func Match(q, mode string) string {
	terms := Terms(q)
	if len(terms) == 0 {
		return ""
	}
	parts := make([]string, 0, len(terms))
	for _, t := range terms {
		s := stem(t)
		// Quote to neutralise FTS5 operators, then add the prefix star.
		parts = append(parts, `"`+strings.ReplaceAll(s, `"`, `""`)+`"*`)
	}
	sep := " OR "
	if mode == "all" {
		sep = " AND "
	}
	return strings.Join(parts, sep)
}

// Fold lowercases s and strips diacritics, the same way search terms are folded.
func Fold(s string) string { return fold(s) }

// Stems returns the folded, stemmed terms of q, in the same form Match uses.
func Stems(q string) []string {
	terms := Terms(q)
	out := make([]string, 0, len(terms))
	for _, t := range terms {
		out = append(out, stem(t))
	}
	return out
}
