package search_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dicapower/noah/internal/memory"
	"github.com/dicapower/noah/internal/search"
	"github.com/dicapower/noah/internal/store"
)

func seed(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "n.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	for _, in := range []memory.Input{
		{Space: "app", Kind: "bugfix", Title: "Duplicate invoices on retry", Content: "Webhook retries created duplicated invoices. Fixed with an idempotency key stored per Stripe event id."},
		{Space: "app", Kind: "decision", Title: "Despliegue con Dokploy", Content: "Decidimos desplegar con Dokploy y Traefik; los dominios apuntan al puerto interno del contenedor."},
		{Space: "app", Kind: "pattern", Title: "Date handling", Content: "Store all timestamps in UTC and convert in the UI layer only."},
		{Space: "app", Kind: "decision", Title: "Auth provider", Content: "We use Firebase Auth with custom claims for roles.", Trust: memory.TrustUser, Pinned: true},
		{Space: "other", Kind: "note", Title: "Unrelated", Content: "Invoices for the other project are generated monthly."},
	} {
		if _, err := s.Save(context.Background(), in); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestNaturalLanguageQuestionsFindTheRightMemory(t *testing.T) {
	s := seed(t)
	cases := []struct{ q, wantTitle string }{
		{"how did we stop duplicated invoices from the webhook?", "Duplicate invoices on retry"},
		{"cómo desplegamos la aplicación y qué dominios usa", "Despliegue con Dokploy"},
		{"what timezone do we store dates in", "Date handling"},
		{"which provider handles login and roles?", "Auth provider"},
	}
	for _, c := range cases {
		hits, err := search.Run(context.Background(), s, c.q, search.Options{Space: "app"})
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) == 0 || hits[0].Memory.Title != c.wantTitle {
			var got []string
			for _, h := range hits {
				got = append(got, h.Memory.Title)
			}
			t.Errorf("%q: want top %q, got %v", c.q, c.wantTitle, got)
		}
	}
}

func TestSpaceScopeAndAll(t *testing.T) {
	s := seed(t)
	in, _ := search.Run(context.Background(), s, "invoices", search.Options{Space: "app"})
	for _, h := range in {
		if h.Memory.Space != "app" {
			t.Fatalf("leaked space %q", h.Memory.Space)
		}
	}
	all, _ := search.Run(context.Background(), s, "invoices", search.Options{})
	spaces := map[string]bool{}
	for _, h := range all {
		spaces[h.Memory.Space] = true
	}
	if !spaces["app"] || !spaces["other"] {
		t.Fatalf("all-space search should cover both spaces, got %v", spaces)
	}
}

func TestKindFilterAndLimit(t *testing.T) {
	s := seed(t)
	hits, _ := search.Run(context.Background(), s, "we decided use", search.Options{Space: "app", Kind: "decision", Limit: 1})
	if len(hits) != 1 || hits[0].Memory.Kind != "decision" {
		t.Fatalf("kind/limit not honoured: %+v", hits)
	}
}

func TestPinnedUserMemoryOutranksEqualMatch(t *testing.T) {
	s, _ := store.Open(filepath.Join(t.TempDir(), "n.db"), store.Options{})
	defer s.Close()
	ctx := context.Background()
	s.Save(ctx, memory.Input{Space: "p", Title: "cache ttl", Content: "cache ttl is 60 seconds", Trust: memory.TrustExternal})
	s.Save(ctx, memory.Input{Space: "p", Title: "cache ttl rule", Content: "cache ttl is 300 seconds", Trust: memory.TrustUser, Pinned: true})
	hits, _ := search.Run(ctx, s, "cache ttl", search.Options{Space: "p"})
	if len(hits) != 2 || !hits[0].Memory.Pinned {
		t.Fatalf("pinned user memory should rank first: %+v", hits)
	}
}

func TestRecencyBreaksTies(t *testing.T) {
	now := time.Now()
	old := now.Add(-400 * 24 * time.Hour)
	dir := t.TempDir()
	clock := old
	s, _ := store.Open(filepath.Join(dir, "n.db"), store.Options{Now: func() time.Time { return clock }})
	defer s.Close()
	ctx := context.Background()
	s.Save(ctx, memory.Input{Space: "p", Title: "queue worker", Content: "queue worker concurrency is 4"})
	clock = now
	s.Save(ctx, memory.Input{Space: "p", Title: "queue worker v2", Content: "queue worker concurrency is 8"})
	hits, _ := search.Run(ctx, s, "queue worker concurrency", search.Options{Space: "p", Now: now})
	if len(hits) != 2 || !strings.Contains(hits[0].Memory.Content, "8") {
		t.Fatalf("newer memory should win a tie: %+v", hits)
	}
}

func TestNoUsableTermsAndNoMatch(t *testing.T) {
	s := seed(t)
	if hits, err := search.Run(context.Background(), s, "   ", search.Options{}); err != nil || hits != nil {
		t.Fatalf("blank query => nil, got %v %v", hits, err)
	}
	if hits, _ := search.Run(context.Background(), s, "zzzxqj", search.Options{}); len(hits) != 0 {
		t.Fatalf("expected no hits, got %d", len(hits))
	}
	// FTS operators in user text must not break the query
	if _, err := search.Run(context.Background(), s, `invoices" OR NEAR( AND * -`, search.Options{}); err != nil {
		t.Fatalf("operators must be neutralised: %v", err)
	}
}

func TestClearWinnerIsNotPaddedWithNoise(t *testing.T) {
	s := seed(t)
	ctx := context.Background()
	for i, c := range []string{
		"The build works fine on the staging machine",
		"This script works only with Node 20",
		"Cache invalidation works through a version key",
	} {
		_, err := s.Save(ctx, memory.Input{Space: "app", Title: "Filler " + string(rune('A'+i)), Content: c})
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := s.Save(ctx, memory.Input{Space: "app", Title: "Zephyr probe", Content: "zephyr probe works from the agent client"})
	if err != nil {
		t.Fatal(err)
	}
	hits, err := search.Run(ctx, s, "zephyr probe works from the agent", search.Options{Space: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Memory.Title != "Zephyr probe" {
		var got []string
		for _, h := range hits {
			got = append(got, h.Memory.Title)
		}
		t.Fatalf("want only the real match, got %v", got)
	}
}

func TestVagueQueryStillReturnsSeveral(t *testing.T) {
	s := seed(t)
	hits, err := search.Run(context.Background(), s, "invoices", search.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) < 2 {
		t.Fatalf("a vague one-word query should keep every comparable match, got %d", len(hits))
	}
}

func TestProjectSearchIncludesGlobalButNotOtherProjects(t *testing.T) {
	s := seed(t)
	ctx := context.Background()
	for _, in := range []memory.Input{
		{Space: memory.Global, Title: "Dokploy hosting", Content: "All client apps are deployed on our Dokploy instance behind Traefik."},
		{Space: "other", Title: "Other Dokploy note", Content: "The other project also uses Dokploy for hosting."},
	} {
		if _, err := s.Save(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	hits, err := search.Run(ctx, s, "where do we host apps with Dokploy", search.Options{Space: "app", Global: true})
	if err != nil {
		t.Fatal(err)
	}
	var spaces []string
	for _, h := range hits {
		spaces = append(spaces, h.Memory.Space)
	}
	if !contains(spaces, memory.Global) || contains(spaces, "other") {
		t.Fatalf("want global but not other projects, got %v", spaces)
	}
	hits, _ = search.Run(ctx, s, "where do we host apps with Dokploy", search.Options{Space: "app"})
	for _, h := range hits {
		if h.Memory.Space == memory.Global {
			t.Fatal("without Global the global space must not be searched")
		}
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

type fixedSource []memory.Scored

func (f fixedSource) Candidates(context.Context, string, string, string, int) ([]memory.Scored, error) {
	return f, nil
}

// A question that names a relative date prefers the note written then, even
// when an older or newer note matches the topic words equally well, and it
// does not prefer the newest note.
func TestRelativeDateSelectsNoteFromThatTime(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	at := func(daysAgo int) int64 { return now.AddDate(0, 0, -daysAgo).UnixMilli() }
	mk := func(id int64, daysAgo int) memory.Scored {
		return memory.Scored{BM25: -1, Memory: memory.Memory{ID: id, Title: "bought blender", Content: "I bought a blender", CreatedAt: at(daysAgo), UpdatedAt: at(daysAgo), Trust: memory.TrustAgent}}
	}
	src := fixedSource{mk(1, 1), mk(2, 14), mk(3, 60)}
	hits, err := search.Run(context.Background(), src, "What did I buy two weeks ago? blender", search.Options{Now: now})
	if err != nil || len(hits) == 0 {
		t.Fatalf("hits=%v err=%v", hits, err)
	}
	if hits[0].Memory.ID != 2 {
		t.Fatalf("top hit is %d, want the note from two weeks ago (2)", hits[0].Memory.ID)
	}
}
