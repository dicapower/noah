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
