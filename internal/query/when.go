package query

import (
	"regexp"
	"strconv"
	"strings"
)

// Window is a span of ages, in days before "now", that a question points at.
type Window struct {
	Lo, Hi float64
}

var (
	numWords = map[string]float64{
		"a": 1, "an": 1, "one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6,
		"seven": 7, "eight": 8, "nine": 9, "ten": 10, "eleven": 11, "twelve": 12,
		"a couple of": 2, "couple of": 2, "a few": 3,
		"un": 1, "una": 1, "dos": 2, "tres": 3, "cuatro": 4, "cinco": 5, "seis": 6,
		"siete": 7, "ocho": 8, "nueve": 9, "diez": 10, "once": 11, "doce": 12,
		"un par de": 2, "unos": 3, "unas": 3,
	}
	numAlt  = `\d+|a couple of|couple of|a few|un par de|a|an|one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve|un|una|unos|unas|dos|tres|cuatro|cinco|seis|siete|ocho|nueve|diez|once|doce`
	unitAlt = `days?|weeks?|months?|years?|dias?|semanas?|semana|mes|meses|anos?`
	agoEN   = regexp.MustCompile(`\b(` + numAlt + `)\s+(` + unitAlt + `)\s+ago\b`)
	agoES   = regexp.MustCompile(`\bhace\s+(` + numAlt + `)\s+(` + unitAlt + `)\b`)
	fixed   = []struct {
		re *regexp.Regexp
		w  Window
	}{
		{regexp.MustCompile(`\b(yesterday|ayer)\b`), Window{0, 2}},
		{regexp.MustCompile(`\b(last week|la semana pasada)\b`), Window{5, 14}},
		{regexp.MustCompile(`\b(last month|el mes pasado)\b`), Window{20, 45}},
		{regexp.MustCompile(`\b(last year|el ano pasado)\b`), Window{300, 430}},
	}
)

// Time finds a relative date in q ("two weeks ago", "hace 10 días", "last
// month") and returns the window of memory ages it points at, plus q with the
// phrase removed so that words like "weeks" and "ago" do not pollute the text
// match. ok is false when q has no relative date.
func Time(q string) (w Window, rest string, ok bool) {
	f := fold(q)
	for _, re := range []*regexp.Regexp{agoEN, agoES} {
		if m := re.FindStringSubmatch(f); m != nil {
			n := parseNum(m[1])
			days, tol := unitDays(m[2])
			c, t := n*days, tol*(1+n/8) // the further back, the vaguer the date
			return Window{max0(c - t), c + t}, strings.Replace(f, m[0], " ", 1), true
		}
	}
	for _, x := range fixed {
		if m := x.re.FindString(f); m != "" {
			return x.w, strings.Replace(f, m, " ", 1), true
		}
	}
	return Window{}, q, false
}

func parseNum(s string) float64 {
	if n, err := strconv.Atoi(s); err == nil {
		return float64(n)
	}
	return numWords[s]
}

// unitDays gives the length of a unit in days and how far either side of the
// exact date a memory still counts: people say "two weeks ago" loosely.
func unitDays(u string) (days, tol float64) {
	switch {
	case strings.HasPrefix(u, "day"), strings.HasPrefix(u, "dia"):
		return 1, 1
	case strings.HasPrefix(u, "week"), strings.HasPrefix(u, "semana"):
		return 7, 3
	case strings.HasPrefix(u, "month"), strings.HasPrefix(u, "mes"):
		return 30, 8
	}
	return 365, 30
}

func max0(f float64) float64 { return maxF(0, f) }

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
