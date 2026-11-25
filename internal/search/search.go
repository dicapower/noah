// Package search finds memories for a natural-language question.
//
// Retrieval has two phases. SQLite FTS5 returns a small set of candidates
// (first requiring every term, then any term if that finds too little), and
// this package reranks only those candidates in Go. Ranking therefore costs
// the same no matter how large the database grows, unlike scoring every match
// in SQL.
package search

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/dicapower/noah/internal/memory"
	"github.com/dicapower/noah/internal/query"
)

// CandidateLimit is how many FTS matches are reranked.
const CandidateLimit = 50

// Source supplies FTS candidates. *store.Store implements it.
type Source interface {
	Candidates(ctx context.Context, space, kind, match string, limit int) ([]memory.Scored, error)
}

// Options control a search.
type Options struct {
	Space string // restrict to this space; empty searches all spaces
	Kind  string // restrict to this kind; empty means any
	Limit int    // results to return (default 8)
	Now   time.Time
}

// Hit is one ranked result.
type Hit struct {
	Memory  memory.Memory `json:"memory"`
	Score   float64       `json:"score"`
	Matched int           `json:"matched_terms"`
	Total   int           `json:"query_terms"`
	Snippet string        `json:"snippet"`

	rel float64 // query-dependent part of the score
}

// Weights of the final score; they sum to 1.
const (
	wBM25     = 0.45
	wCoverage = 0.25
	wRecency  = 0.12
	wTrust    = 0.10
	wPinned   = 0.08
	halfLife  = 90.0 // days after which the recency signal has decayed to 1/e

	// minRelevance drops a hit whose query-dependent relevance (BM25 share plus
	// term coverage) is below this fraction of the best hit's. Recency, trust and
	// pinned only order hits that already match; they never keep a weak one alive.
	minRelevance = 0.4
)

// Run searches for q and returns up to opt.Limit hits, best first.
func Run(ctx context.Context, src Source, q string, opt Options) ([]Hit, error) {
	if opt.Limit <= 0 {
		opt.Limit = 8
	}
	if opt.Now.IsZero() {
		opt.Now = time.Now()
	}
	stems := query.Stems(q)
	if len(stems) == 0 {
		return nil, nil
	}
	cands, err := src.Candidates(ctx, opt.Space, opt.Kind, query.Match(q, "all"), CandidateLimit)
	if err != nil {
		return nil, err
	}
	if len(cands) < opt.Limit && len(stems) > 1 {
		more, err := src.Candidates(ctx, opt.Space, opt.Kind, query.Match(q, "any"), CandidateLimit)
		if err != nil {
			return nil, err
		}
		cands = merge(cands, more)
	}
	return rerank(cands, stems, opt), nil
}

func merge(a, b []memory.Scored) []memory.Scored {
	seen := make(map[int64]bool, len(a))
	for _, c := range a {
		seen[c.Memory.ID] = true
	}
	for _, c := range b {
		if !seen[c.Memory.ID] {
			a = append(a, c)
		}
	}
	return a
}

func rerank(cands []memory.Scored, stems []string, opt Options) []Hit {
	if len(cands) == 0 {
		return nil
	}
	// BM25 from FTS5 is negative and lower is better. Score each candidate as a
	// fraction of the best one, so near-equal matches get near-equal relevance
	// and the other signals (recency, trust, pinned) can break the tie. Min-max
	// scaling would blow a tiny difference up to the full range.
	best := 0.0
	for _, c := range cands {
		best = math.Min(best, c.BM25)
	}
	hits := make([]Hit, 0, len(cands))
	for _, c := range cands {
		m := c.Memory
		bm := 1.0
		if best < 0 {
			bm = math.Max(0, math.Min(1, c.BM25/best))
		}
		text := query.Fold(m.Title + "\n" + m.Content + "\n" + strings.Join(m.Tags, " "))
		matched := 0
		for _, s := range stems {
			if strings.Contains(text, s) {
				matched++
			}
		}
		cov := float64(matched) / float64(len(stems))
		ageDays := math.Max(0, opt.Now.Sub(time.UnixMilli(m.UpdatedAt)).Hours()/24)
		rec := math.Exp(-ageDays / halfLife)
		pin := 0.0
		if m.Pinned {
			pin = 1
		}
		rel := wBM25*bm + wCoverage*cov
		score := rel + wRecency*rec + wTrust*m.Trust.Rank() + wPinned*pin
		hits = append(hits, Hit{Memory: m, Score: score, rel: rel, Matched: matched, Total: len(stems), Snippet: snippet(m.Content, stems)})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Memory.ID > hits[j].Memory.ID
	})
	bestRel := 0.0
	for _, h := range hits {
		bestRel = math.Max(bestRel, h.rel)
	}
	kept := hits[:0]
	for _, h := range hits {
		if h.rel >= minRelevance*bestRel {
			kept = append(kept, h)
		}
	}
	hits = kept
	if len(hits) > opt.Limit {
		hits = hits[:opt.Limit]
	}
	return hits
}

// snippet returns about 220 characters of content around the first matching term.
func snippet(content string, stems []string) string {
	flat := strings.Join(strings.Fields(content), " ")
	runes := []rune(flat)
	const width = 220
	if len(runes) <= width {
		return memory.Sanitize(flat, 0)
	}
	folded := []rune(query.Fold(flat))
	pos := 0
	if len(folded) == len(runes) { // folding kept rune positions aligned
		best := -1
		for _, s := range stems {
			if i := indexRunes(folded, []rune(s)); i >= 0 && (best < 0 || i < best) {
				best = i
			}
		}
		if best > 0 {
			pos = best
		}
	}
	start := pos - width/4
	if start < 0 {
		start = 0
	}
	end := start + width
	if end > len(runes) {
		end = len(runes)
		start = maxInt(0, end-width)
	}
	out := string(runes[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(runes) {
		out += "…"
	}
	return memory.Sanitize(out, 0)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func indexRunes(hay, needle []rune) int {
	if len(needle) == 0 {
		return -1
	}
	for i := 0; i+len(needle) <= len(hay); i++ {
		j := 0
		for j < len(needle) && hay[i+j] == needle[j] {
			j++
		}
		if j == len(needle) {
			return i
		}
	}
	return -1
}
