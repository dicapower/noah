package query

import (
	"regexp"
	"strconv"
	"strings"
	"time"
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
//
// It also understands calendar dates ("2026-03-03", "March 3rd", "3 de marzo")
// and "last Tuesday" / "el martes pasado", resolved against now.
func Time(q string, now time.Time) (w Window, rest string, ok bool) {
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
	if w, m, ok := absolute(f, now); ok {
		return w, strings.Replace(f, m, " ", 1), true
	}
	return Window{}, q, false
}

var (
	months = map[string]time.Month{
		"january": 1, "february": 2, "march": 3, "april": 4, "may": 5, "june": 6, "july": 7,
		"august": 8, "september": 9, "october": 10, "november": 11, "december": 12,
		"enero": 1, "febrero": 2, "marzo": 3, "abril": 4, "mayo": 5, "junio": 6, "julio": 7,
		"agosto": 8, "septiembre": 9, "setiembre": 9, "octubre": 10, "noviembre": 11, "diciembre": 12,
	}
	days = map[string]time.Weekday{
		"sunday": 0, "monday": 1, "tuesday": 2, "wednesday": 3, "thursday": 4, "friday": 5, "saturday": 6,
		"domingo": 0, "lunes": 1, "martes": 2, "miercoles": 3, "jueves": 4, "viernes": 5, "sabado": 6,
	}
	monthAlt = `january|february|march|april|may|june|july|august|september|october|november|december|enero|febrero|marzo|abril|mayo|junio|julio|agosto|septiembre|setiembre|octubre|noviembre|diciembre`
	dayAlt   = `sunday|monday|tuesday|wednesday|thursday|friday|saturday|domingo|lunes|martes|miercoles|jueves|viernes|sabado`
	isoDate  = regexp.MustCompile(`\b(\d{4})-(\d{2})-(\d{2})\b`)
	dayFirst = regexp.MustCompile(`\b(\d{1,2})(?:st|nd|rd|th)?\s+(?:of\s+|de\s+)?(` + monthAlt + `)\b(?:,?\s+(?:de\s+)?(\d{4})\b)?`)
	monthDay = regexp.MustCompile(`\b(` + monthAlt + `)\s+(\d{1,2})(?:st|nd|rd|th)?\b(?:,?\s+(\d{4})\b)?`)
	lastDay  = regexp.MustCompile(`\b(?:last\s+(` + dayAlt + `)|el\s+(` + dayAlt + `)\s+pasado)\b`)
)

// absolute resolves a calendar date or "last <weekday>" in the folded text f.
func absolute(f string, now time.Time) (Window, string, bool) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	around := func(d time.Time) (Window, bool) {
		age := today.Sub(d).Hours() / 24
		if age < 0 {
			return Window{}, false
		}
		return Window{max0(age - 1), age + 1}, true
	}
	mk := func(y, mo, d int, yearGiven bool) (Window, bool) {
		if mo < 1 || mo > 12 || d < 1 || d > 31 {
			return Window{}, false
		}
		date := time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC)
		if date.Day() != d { // 31 February and the like
			return Window{}, false
		}
		if !yearGiven && date.After(today) {
			date = date.AddDate(-1, 0, 0)
		}
		return around(date)
	}
	if m := isoDate.FindStringSubmatch(f); m != nil {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		if w, ok := mk(y, mo, d, true); ok {
			return w, m[0], true
		}
	}
	for _, re := range []*regexp.Regexp{dayFirst, monthDay} {
		m := re.FindStringSubmatch(f)
		if m == nil {
			continue
		}
		name, dd := m[2], m[1]
		if re == monthDay {
			name, dd = m[1], m[2]
		}
		d, _ := strconv.Atoi(dd)
		y, given := now.Year(), false
		if m[3] != "" {
			y, _ = strconv.Atoi(m[3])
			given = true
		}
		if w, ok := mk(y, int(months[name]), d, given); ok {
			return w, m[0], true
		}
	}
	if m := lastDay.FindStringSubmatch(f); m != nil {
		name := m[1] + m[2]
		n := (int(today.Weekday()) - int(days[name]) + 7) % 7
		if n == 0 {
			n = 7
		}
		return Window{max0(float64(n) - 1), float64(n) + 1}, m[0], true
	}
	return Window{}, "", false
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
