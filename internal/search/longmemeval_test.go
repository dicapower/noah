package search_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dicapower/noah/internal/memory"
	"github.com/dicapower/noah/internal/search"
	"github.com/dicapower/noah/internal/store"
)

// TestLongMemEval measures retrieval on LongMemEval-S (Wu et al., 2024), the
// public benchmark most memory tools quote. It never asserts: it reports.
//
//	NOAH_LONGMEMEVAL=/path/longmemeval_s_cleaned.json go test ./internal/search -run TestLongMemEval -v
//
// Optional: NOAH_LME_GRAN=session|round (default session), NOAH_LME_N=100 to use
// only the first 100 questions, NOAH_LME_MISSES=1 to list the questions missed
// in the top 5.
//
// Method. Each question gets its own empty database holding its ~48 history
// sessions, saved through the normal Save path (secret redaction included).
// The question is asked verbatim through search.Run. A question is a hit@k when
// one of the first k results comes from an answer session
// (answer_session_ids). Abstention questions (id ending _abs) have no answer
// session and are skipped, as in the benchmark's own retrieval evaluation.
//
// Granularity "session" saves one memory per session; "round" saves one memory
// per user/assistant exchange, which is closer to the size of a real note.
// Recency is measured against the question date, as it would be in real use.
func TestLongMemEval(t *testing.T) {
	path := os.Getenv("NOAH_LONGMEMEVAL")
	if path == "" {
		t.Skip("set NOAH_LONGMEMEVAL to the longmemeval_s_cleaned.json path to run")
	}
	gran := os.Getenv("NOAH_LME_GRAN")
	if gran == "" {
		gran = "session"
	}
	if gran != "session" && gran != "round" {
		t.Fatalf("NOAH_LME_GRAN must be session or round, got %q", gran)
	}
	limitN, _ := strconv.Atoi(os.Getenv("NOAH_LME_N"))

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	if _, err := dec.Token(); err != nil { // the opening [
		t.Fatalf("%s is not a JSON array: %v", path, err)
	}

	jobs := make(chan lmeQuestion)
	var mu sync.Mutex
	var results []lmeResult
	var wg sync.WaitGroup
	for w := 0; w < runtime.NumCPU(); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for q := range jobs {
				r, err := runLME(q, gran)
				if err != nil {
					t.Errorf("question %s: %v", q.ID, err)
					continue
				}
				mu.Lock()
				results = append(results, r)
				mu.Unlock()
			}
		}()
	}
	start := time.Now()
	read, skipped := 0, 0
	for dec.More() && (limitN == 0 || read < limitN) {
		var q lmeQuestion
		if err := dec.Decode(&q); err != nil {
			t.Fatal(err)
		}
		read++
		if strings.HasSuffix(q.ID, "_abs") {
			skipped++
			continue
		}
		jobs <- q
	}
	close(jobs)
	wg.Wait()
	if len(results) == 0 {
		t.Fatal("no questions were evaluated")
	}
	reportLME(t, gran, results, skipped, time.Since(start))
}

type lmeQuestion struct {
	ID           string      `json:"question_id"`
	Type         string      `json:"question_type"`
	Question     string      `json:"question"`
	Date         string      `json:"question_date"`
	AnswerIDs    []string    `json:"answer_session_ids"`
	SessionIDs   []string    `json:"haystack_session_ids"`
	SessionDates []string    `json:"haystack_dates"`
	Sessions     [][]lmeTurn `json:"haystack_sessions"`
}

type lmeTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type lmeResult struct {
	id                  string
	typ                 string
	top1, top5, top10   bool
	memories, truncated int
}

const lmeDateLayout = "2006/01/02 (Mon) 15:04"

// lmeMaxBytes leaves room under memory.MaxContent for the longest note.
const lmeMaxBytes = memory.MaxContent - 1024

func runLME(q lmeQuestion, gran string) (lmeResult, error) {
	res := lmeResult{id: q.ID, typ: q.Type}
	dir, err := os.MkdirTemp("", "lme")
	if err != nil {
		return res, err
	}
	defer os.RemoveAll(dir)
	s, err := store.Open(filepath.Join(dir, "lme.db"), store.Options{})
	if err != nil {
		return res, err
	}
	defer s.Close()
	ctx := context.Background()

	owner := map[int64][]string{} // memory id -> session ids it came from
	save := func(sessionID, text string, at time.Time) error {
		text = strings.TrimSpace(text)
		if text == "" {
			return nil
		}
		if len(text) > lmeMaxBytes {
			text = strings.ToValidUTF8(text[:lmeMaxBytes], "")
			res.truncated++
		}
		r, err := s.Save(ctx, memory.Input{Space: "lme", Content: text, CreatedAt: at, UpdatedAt: at})
		if err != nil {
			return err
		}
		owner[r.Memory.ID] = append(owner[r.Memory.ID], sessionID)
		res.memories++
		return nil
	}
	for i, sess := range q.Sessions {
		sid := q.SessionIDs[i]
		at, _ := time.Parse(lmeDateLayout, q.SessionDates[i])
		switch gran {
		case "session":
			var b strings.Builder
			for _, turn := range sess {
				b.WriteString(turn.Content)
				b.WriteString("\n\n")
			}
			if err := save(sid, b.String(), at); err != nil {
				return res, err
			}
		case "round":
			for j := 0; j < len(sess); j += 2 {
				text := sess[j].Content
				if j+1 < len(sess) {
					text += "\n\n" + sess[j+1].Content
				}
				if err := save(sid, text, at); err != nil {
					return res, err
				}
			}
		}
	}

	now, _ := time.Parse(lmeDateLayout, q.Date)
	hits, err := search.Run(ctx, s, q.Question, search.Options{Space: "lme", Limit: 10, Now: now})
	if err != nil {
		return res, err
	}
	answer := map[string]bool{}
	for _, id := range q.AnswerIDs {
		answer[id] = true
	}
	for i, h := range hits {
		hit := false
		for _, sid := range owner[h.Memory.ID] {
			hit = hit || answer[sid]
		}
		if !hit {
			continue
		}
		res.top1 = res.top1 || i == 0
		res.top5 = res.top5 || i < 5
		res.top10 = true
	}
	return res, nil
}

func reportLME(t *testing.T, gran string, rs []lmeResult, skipped int, took time.Duration) {
	t.Helper()
	by := map[string][]lmeResult{}
	for _, r := range rs {
		by[r.typ] = append(by[r.typ], r)
	}
	types := make([]string, 0, len(by))
	for k := range by {
		types = append(types, k)
	}
	sort.Strings(types)
	pct := func(rs []lmeResult, f func(lmeResult) bool) float64 {
		n := 0
		for _, r := range rs {
			if f(r) {
				n++
			}
		}
		return float64(n) / float64(len(rs))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\nLongMemEval-S, granularity=%s, %d questions (%d abstention skipped), %s\n", gran, len(rs), skipped, took.Round(time.Second))
	fmt.Fprintf(&b, "%-28s %5s %8s %8s %8s\n", "question type", "n", "hit@1", "hit@5", "hit@10")
	row := func(name string, rs []lmeResult) {
		fmt.Fprintf(&b, "%-28s %5d %8.3f %8.3f %8.3f\n", name, len(rs),
			pct(rs, func(r lmeResult) bool { return r.top1 }),
			pct(rs, func(r lmeResult) bool { return r.top5 }),
			pct(rs, func(r lmeResult) bool { return r.top10 }))
	}
	for _, k := range types {
		row(k, by[k])
	}
	row("ALL", rs)
	if os.Getenv("NOAH_LME_MISSES") != "" {
		var miss []string
		for _, r := range rs {
			if !r.top5 {
				miss = append(miss, r.typ+":"+r.id)
			}
		}
		sort.Strings(miss)
		fmt.Fprintf(&b, "missed at 5 (%d): %s\n", len(miss), strings.Join(miss, " "))
	}
	mem, trunc := 0, 0
	for _, r := range rs {
		mem += r.memories
		trunc += r.truncated
	}
	fmt.Fprintf(&b, "memories per question: %d on average; truncated to fit one memory: %d\n", mem/len(rs), trunc)
	fmt.Print(b.String())
}
