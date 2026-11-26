package query

import (
	"reflect"
	"strings"
	"testing"
)

func TestTermsDropsStopwordsAndFoldsAccents(t *testing.T) {
	got := Terms("¿Por qué elegimos Dokploy para desplegar?")
	want := []string{"elegimos", "dokploy", "desplegar"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestTermsFallbackWhenOnlyStopwords(t *testing.T) {
	if got := Terms("how to"); len(got) == 0 {
		t.Fatal("expected raw-word fallback")
	}
}

func TestMatchNeutralisesOperators(t *testing.T) {
	m := Match(`foo" OR NEAR(bar) -baz *`, "any")
	for _, bad := range []string{"NEAR(", `"foo"" OR`} {
		if contains(m, bad) {
			t.Fatalf("unsafe query: %s", m)
		}
	}
	if m == "" {
		t.Fatal("empty")
	}
}

func TestMatchModes(t *testing.T) {
	if got := Match("duplicate invoice", "all"); got != `"duplic"* AND "invoi"*` {
		t.Fatalf("all: %s", got)
	}
	if got := Match("duplicate invoice", "any"); got != `"duplic"* OR "invoi"*` {
		t.Fatalf("any: %s", got)
	}
	if Match("", "any") != "" {
		t.Fatal("empty query must give empty match")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestStemKeepsIdentifiersWhole(t *testing.T) {
	for _, w := range []string{"zorblax12", "ticket123", "sha256", "v2", "eng1234"} {
		if got := stem(w); got != w {
			t.Errorf("stem(%q) = %q; identifiers with digits must not be trimmed", w, got)
		}
	}
	if got := stem("invoices"); got != "invoic" {
		t.Errorf("plain words are still stemmed, got %q", got)
	}
}

func TestMatchIdentifiersAreExact(t *testing.T) {
	if got := Match("fix ENG-123 invoices", "any"); got != `"fix"* OR "eng"* OR "123" OR "invoic"*` {
		t.Errorf("got %s", got)
	}
}

func TestTime(t *testing.T) {
	cases := []struct {
		q      string
		ok     bool
		lo, hi float64
		rest   string
	}{
		{"What kitchen appliance did I buy 10 days ago?", true, 8, 12, "what kitchen appliance did i buy   ?"},
		{"I mentioned it four weeks ago", true, 25, 31, ""},
		{"¿Qué compré hace dos semanas?", true, 12, 16, ""},
		{"a couple of days ago", true, 1, 3, ""},
		{"how did we fix the retry bug", false, 0, 0, ""},
		{"how many weeks did the migration take", false, 0, 0, ""},
		{"qué pasó ayer con el despliegue", true, 0, 2, ""},
	}
	for _, c := range cases {
		w, rest, ok := Time(c.q)
		if ok != c.ok {
			t.Fatalf("%q: ok=%v want %v", c.q, ok, c.ok)
		}
		if !ok {
			continue
		}
		if w.Lo > c.lo || w.Hi < c.hi {
			t.Errorf("%q: window %v does not cover %v..%v", c.q, w, c.lo, c.hi)
		}
		if strings.Contains(rest, "ago") || strings.Contains(rest, "hace") {
			t.Errorf("%q: phrase not removed: %q", c.q, rest)
		}
	}
}

func TestTimeEdgeCases(t *testing.T) {
	for _, q := range []string{"hace 3 meses", "hace un año", "99999999999 days ago", "0 days ago", "hace 0 días", "ago", "hace", "last week", ""} {
		w, _, ok := Time(q)
		if ok && (w.Lo < 0 || w.Hi < w.Lo) {
			t.Errorf("%q: bad window %v", q, w)
		}
	}
	if _, _, ok := Time("ago"); ok {
		t.Error("bare 'ago' must not be a date")
	}
	if w, _, ok := Time("hace 3 meses"); !ok || w.Lo > 90 || w.Hi < 90 {
		t.Errorf("3 months should cover 90 days: %v %v", w, ok)
	}
}
