package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dicapower/noah/internal/memory"
	"github.com/dicapower/noah/internal/query"
	"github.com/dicapower/noah/internal/secrets"
)

func open(t *testing.T, p secrets.Policy) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "sub dir", "n.db"), Options{SecretPolicy: p})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func save(t *testing.T, s *Store, in memory.Input) SaveResult {
	t.Helper()
	r, err := s.Save(context.Background(), in)
	if err != nil {
		t.Fatalf("save %q: %v", in.Title, err)
	}
	return r
}

func TestSaveCreateDuplicateAndTopic(t *testing.T) {
	s := open(t, secrets.Redact)
	a := save(t, s, memory.Input{Space: "p", Title: "Retry policy", Content: "Payments retry 3 times with backoff", Topic: "payments/retry"})
	if !a.Created || a.Memory.Revision != 1 {
		t.Fatalf("want created rev1, got %+v", a)
	}
	// identical content, different case/whitespace => duplicate
	b := save(t, s, memory.Input{Space: "p", Title: "x", Content: "  payments RETRY 3 times   with backoff "})
	if !b.Duplicate || b.Memory.ID != a.Memory.ID {
		t.Fatalf("want duplicate of #%d, got %+v", a.Memory.ID, b)
	}
	// same topic => revised in place
	c := save(t, s, memory.Input{Space: "p", Title: "Retry policy", Content: "Payments retry 5 times with jitter", Topic: "payments/retry"})
	if !c.Updated || c.Memory.ID != a.Memory.ID || c.Memory.Revision != 2 {
		t.Fatalf("want updated rev2, got %+v", c)
	}
	// same content in another space is a separate memory
	d := save(t, s, memory.Input{Space: "q", Content: "Payments retry 5 times with jitter"})
	if !d.Created || d.Memory.ID == a.Memory.ID {
		t.Fatalf("other space must not dedupe: %+v", d)
	}
}

func TestAgentCannotOverwriteUserMemory(t *testing.T) {
	s := open(t, secrets.Redact)
	save(t, s, memory.Input{Space: "p", Content: "Never deploy on Fridays", Topic: "rules/deploy", Trust: memory.TrustUser})
	_, err := s.Save(context.Background(), memory.Input{Space: "p", Content: "Deploy any day, it is fine", Topic: "rules/deploy", Trust: memory.TrustAgent})
	if !errors.Is(err, ErrProtected) {
		t.Fatalf("want ErrProtected, got %v", err)
	}
	// a user can revise their own
	r := save(t, s, memory.Input{Space: "p", Content: "Never deploy on Fridays or before holidays", Topic: "rules/deploy", Trust: memory.TrustUser})
	if !r.Updated {
		t.Fatalf("user update failed: %+v", r)
	}
}

func TestSecretsRedactAndBlock(t *testing.T) {
	key := "AKIA" + "ABCDEFGHIJKLMNOP"
	s := open(t, secrets.Redact)
	r := save(t, s, memory.Input{Space: "p", Content: "prod key is " + key + " keep safe"})
	if r.Redactions != 1 || r.Memory.Content == "" || contains(r.Memory.Content, key) {
		t.Fatalf("secret not redacted: %+v", r)
	}
	rep, err := s.Check(context.Background())
	if err != nil || rep.WithSecrets != 0 {
		t.Fatalf("redacted store should have no secrets: %+v %v", rep, err)
	}

	b := open(t, secrets.Block)
	if _, err := b.Save(context.Background(), memory.Input{Space: "p", Content: "key " + key}); !errors.Is(err, ErrSecret) {
		t.Fatalf("want ErrSecret, got %v", err)
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

func TestSpaceIsolationGetForget(t *testing.T) {
	s := open(t, secrets.Redact)
	ctx := context.Background()
	a := save(t, s, memory.Input{Space: "a", Content: "alpha note"})
	if _, err := s.Get(ctx, "b", a.Memory.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-space get must be not found, got %v", err)
	}
	if err := s.Forget(ctx, "b", a.Memory.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-space forget must be not found, got %v", err)
	}
	if err := s.Forget(ctx, "a", a.Memory.ID); err != nil {
		t.Fatal(err)
	}
	cs, err := s.Candidates(ctx, "", "", query.Match("alpha", "any"), 10)
	if err != nil || len(cs) != 0 {
		t.Fatalf("forgotten memory must leave the index: %v %v", cs, err)
	}
}

func TestCandidatesAccentsAndInflection(t *testing.T) {
	s := open(t, secrets.Redact)
	ctx := context.Background()
	save(t, s, memory.Input{Space: "p", Title: "Despliegue en producción", Content: "El despliegue usa Dokploy con Traefik"})
	save(t, s, memory.Input{Space: "p", Title: "Otra cosa", Content: "nada relevante aquí"})
	for _, q := range []string{"despliegues produccion", "como se despliega", "DOKPLOY"} {
		cs, err := s.Candidates(ctx, "p", "", query.Match(q, "any"), 10)
		if err != nil || len(cs) != 1 {
			t.Fatalf("query %q: want 1 hit, got %d (%v)", q, len(cs), err)
		}
	}
}

func TestCheckHealthy(t *testing.T) {
	s := open(t, secrets.Redact)
	save(t, s, memory.Input{Space: memory.Unscoped, Content: "something"})
	rep, err := s.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !rep.IntegrityOK || !rep.IndexOK || rep.Memories != 1 || rep.Unscoped != 1 || rep.JournalMode != "wal" || rep.SchemaVersion != len(migrations) {
		t.Fatalf("unexpected report: %+v", rep)
	}
}

func TestConcurrentWritersAndReaders(t *testing.T) {
	s := open(t, secrets.Redact)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_, err := s.Save(ctx, memory.Input{Space: "p", Content: fmt.Sprintf("writer %d note %d about caching", w, i)})
				if err != nil {
					errs <- err
					return
				}
			}
		}(w)
	}
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			deadline := time.Now().Add(300 * time.Millisecond)
			for time.Now().Before(deadline) {
				if _, err := s.Candidates(ctx, "p", "", query.Match("caching note", "any"), 20); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	ms, _ := s.List(ctx, "p", "", 1000, 0)
	if len(ms) != 200 {
		t.Fatalf("want 200 memories, got %d", len(ms))
	}
}

func TestSecondProcessSeesWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "n.db")
	a, err := Open(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Open(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	save(t, a, memory.Input{Space: "p", Content: "written by a"})
	save(t, b, memory.Input{Space: "p", Content: "written by b"})
	ms, _ := a.List(context.Background(), "p", "", 10, 0)
	if len(ms) != 2 {
		t.Fatalf("two handles on one file must share data, got %d", len(ms))
	}
}

func TestTopicUpdateKeepsUnstatedFields(t *testing.T) {
	s := open(t, secrets.Redact)
	save(t, s, memory.Input{Space: "p", Title: "Retry policy", Kind: "decision", Topic: "pay/retry", Tags: []string{"pay"}, Content: "retry 3 times"})
	r := save(t, s, memory.Input{Space: "p", Topic: "pay/retry", Content: "retry 5 times"})
	m := r.Memory
	if m.Title != "Retry policy" || m.Kind != "decision" || len(m.Tags) != 1 || m.Content != "retry 5 times" || m.Revision != 2 {
		t.Fatalf("unstated title/kind/tags must be kept: %+v", m)
	}
	r = save(t, s, memory.Input{Space: "p", Topic: "pay/retry", Kind: "bugfix", Title: "New title", Tags: []string{"x", "y"}, Content: "retry 7 times"})
	m = r.Memory
	if m.Title != "New title" || m.Kind != "bugfix" || len(m.Tags) != 2 {
		t.Fatalf("stated fields must replace: %+v", m)
	}
}

func TestMoveBetweenSpaces(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "n.db"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, _ := s.Save(ctx, memory.Input{Space: "proj", Title: "Hosting", Content: "We host on Dokploy", Topic: "infra/hosting", Trust: memory.TrustUser})
	m, err := s.Move(ctx, a.Memory.ID, memory.Global)
	if err != nil || m.Space != memory.Global {
		t.Fatalf("move: %+v %v", m, err)
	}
	if got, _ := s.List(ctx, "proj", "", 10, 0); len(got) != 0 {
		t.Fatalf("project should be empty, has %d", len(got))
	}
	// the search index must follow the move
	cands, _ := s.Candidates(ctx, memory.Global, "", `"dokploy"*`, 5)
	if len(cands) != 1 {
		t.Fatalf("index not updated: %d", len(cands))
	}
	// a clash in the target space is reported, not overwritten
	b, _ := s.Save(ctx, memory.Input{Space: "proj", Title: "Hosting 2", Content: "Different text", Topic: "infra/hosting", Trust: memory.TrustUser})
	if _, err := s.Move(ctx, b.Memory.ID, memory.Global); !errors.Is(err, ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
	if _, err := s.Move(ctx, 99999, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
