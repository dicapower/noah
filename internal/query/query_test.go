package query

import (
	"reflect"
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
