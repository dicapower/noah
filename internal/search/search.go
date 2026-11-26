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
	"unicode"

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
	// Global also searches the global space when Space is a project.
	Global bool
	Now    time.Time
	// Embedder, when set, adds a meaning-based signal: candidates whose text is
	// close in embedding space to the question rank higher. It is optional;
	// without it search is lexical only.
	Embedder Embedder
	// SemWeight is the share of relevance given to that signal (default 0.3).
	SemWeight float64
}

// Embedder turns text into a unit-length vector, or nil when it cannot.
type Embedder interface {
	Embed(text string) []float32
}

// semText bounds how much of a long note is embedded per candidate.
const semText = 6000

// semChunk is the target size, in characters, of one embedded passage.
const semChunk = 300

// chunks splits text on white space into passages of about n characters.
func chunks(text string, n int) []string {
	var out []string
	var cur strings.Builder
	for _, w := range strings.Fields(text) {
		if cur.Len() > 0 && cur.Len()+1+len(w) > n {
			out = append(out, cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteByte(' ')
		}
		cur.WriteString(w)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
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
	wBM25     = 0.40
	wCoverage = 0.22 // share of the question's terms found, rare terms counting more
	wTitle    = 0.08 // share of the question's terms found in the title
	wRecency  = 0.12
	wTrust    = 0.10
	wPinned   = 0.08
	halfLife  = 90.0 // days after which the recency signal has decayed to 1/e

	// wTime is the share of relevance given to "was it created when the question
	// says" when the question names a relative date ("two weeks ago"). In that
	// case recency is switched off: the question asks about a moment in the
	// past, not about the latest note.
	wTime = 0.35

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
	// A relative date in the question ("hace dos semanas") says when the note
	// was written, not what it says, so it is lifted out of the text match.
	win, rest, timed := query.Time(q, opt.Now)
	if timed && len(query.Stems(rest)) > 0 {
		q = rest
	}
	stems := query.Stems(q)
	if len(stems) == 0 {
		return nil, nil
	}
	spaces := []string{opt.Space}
	if opt.Global && opt.Space != "" && opt.Space != memory.Global {
		spaces = append(spaces, memory.Global)
	}
	collect := func(mode string) ([]memory.Scored, error) {
		var all []memory.Scored
		for _, sp := range spaces {
			c, err := src.Candidates(ctx, sp, opt.Kind, query.Match(q, mode), CandidateLimit)
			if err != nil {
				return nil, err
			}
			all = merge(all, c)
		}
		return all, nil
	}
	cands, err := collect("all")
	if err != nil {
		return nil, err
	}
	// Always widen to "any term" too. Stopping after the AND phase finds enough
	// results hides the note that matches the rare terms of the question but
	// lacks one of its common words (e.g. "configuramos", "producción").
	if len(stems) > 1 {
		more, err := collect("any")
		if err != nil {
			return nil, err
		}
		cands = merge(cands, more)
	}
	var tw *query.Window
	if timed {
		tw = &win
	}
	return rerank(cands, stems, query.Terms(q), q, opt, tw), nil
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

func rerank(cands []memory.Scored, stems, terms []string, question string, opt Options, when *query.Window) []Hit {
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
	// Inverse document frequency among the candidates: a term that nearly every
	// candidate contains ("producción") says little about which one answers the
	// question, while a term only one candidate contains is the real signal.
	texts := make([]string, len(cands))
	df := make([]int, len(stems))
	for i, c := range cands {
		m := c.Memory
		texts[i] = query.Fold(m.Title + "\n" + m.Content + "\n" + strings.Join(m.Tags, " "))
		for j, s := range stems {
			if strings.Contains(texts[i], s) {
				df[j]++
			}
		}
	}
	idf := make([]float64, len(stems))
	idfSum := 0.0
	for j := range stems {
		idf[j] = math.Log(1 + float64(len(cands))/float64(maxInt(df[j], 1)))
		idfSum += idf[j]
	}
	sem := semanticFit(cands, question, opt)
	hits := make([]Hit, 0, len(cands))
	for i, c := range cands {
		m := c.Memory
		bm := 1.0
		if best < 0 {
			bm = math.Max(0, math.Min(1, c.BM25/best))
		}
		title := query.Fold(m.Title)
		words := wordSet(texts[i])
		titleWords := wordSet(title)
		matched := 0
		covW, titleW := 0.0, 0.0
		for j, s := range stems {
			// A whole-word match counts fully; matching only as a prefix (the
			// stem of "factura" also finds "facturado", and a name like
			// "Povetu" also finds "povetuzi") counts half.
			if w := matchWeight(words, texts[i], terms[j], s); w > 0 {
				matched++
				covW += idf[j] * w
			}
			if w := matchWeight(titleWords, title, terms[j], s); w > 0 {
				titleW += w
			}
		}
		cov := covW / idfSum
		titleCov := titleW / float64(len(stems))
		ageDays := math.Max(0, opt.Now.Sub(time.UnixMilli(m.UpdatedAt)).Hours()/24)
		rec := math.Exp(-ageDays / halfLife)
		pin := 0.0
		if m.Pinned {
			pin = 1
		}
		rel := wBM25*bm + wCoverage*cov + wTitle*titleCov
		if sem != nil {
			rel = (1-opt.semWeight())*rel + opt.semWeight()*sem[i]
		}
		wRec := wRecency
		if when != nil {
			created := math.Max(0, opt.Now.Sub(time.UnixMilli(m.CreatedAt)).Hours()/24)
			rel = (1-wTime)*rel + wTime*timeFit(created, *when)
			wRec = 0
		}
		score := rel + wRec*rec + wTrust*m.Trust.Rank() + wPinned*pin
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

func (o Options) semWeight() float64 {
	if o.SemWeight > 0 {
		return o.SemWeight
	}
	return 0.3
}

// semanticFit scores each candidate from 0 to 1 by how close its embedding is
// to the question's, relative to the other candidates. It returns nil when no
// embedder is set or the question has no vector.
func semanticFit(cands []memory.Scored, question string, opt Options) []float64 {
	if opt.Embedder == nil {
		return nil
	}
	qv := opt.Embedder.Embed(question)
	if qv == nil {
		return nil
	}
	cos := make([]float64, len(cands))
	lo, hi := math.Inf(1), math.Inf(-1)
	for i, c := range cands {
		cos[i] = math.NaN()
		text := c.Memory.Title + "\n" + c.Memory.Content
		if r := []rune(text); len(r) > semText {
			text = string(r[:semText])
		}
		// A long note averages into a vague vector; the best short passage says
		// more about whether it contains what the question is after.
		best, any := math.Inf(-1), false
		for _, chunk := range chunks(text, semChunk) {
			if dv := opt.Embedder.Embed(chunk); dv != nil {
				best, any = math.Max(best, dot(qv, dv)), true
			}
		}
		if any {
			cos[i] = best
			lo, hi = math.Min(lo, best), math.Max(hi, best)
		}
	}
	out := make([]float64, len(cands))
	if hi-lo < 1e-9 {
		return nil
	}
	for i := range cands {
		if !math.IsNaN(cos[i]) {
			out[i] = (cos[i] - lo) / (hi - lo)
		}
	}
	return out
}

func dot(a, b []float32) float64 {
	var s float64
	for i := range a {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}

// timeFit is 1 when a memory of the given age falls inside the window and
// fades linearly to 0 over one more window width outside it.
func timeFit(age float64, w query.Window) float64 {
	if age >= w.Lo && age <= w.Hi {
		return 1
	}
	dist := w.Lo - age
	if age > w.Hi {
		dist = age - w.Hi
	}
	return math.Max(0, 1-dist/math.Max(w.Hi-w.Lo, 2))
}

func wordSet(folded string) map[string]bool {
	set := map[string]bool{}
	for _, w := range strings.FieldsFunc(folded, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' }) {
		set[w] = true
	}
	return set
}

// matchWeight is 1 for a whole-word match of term, 0.5 for any other
// occurrence of its stem, 0 for none.
func matchWeight(words map[string]bool, text, term, stem string) float64 {
	if words[term] {
		return 1
	}
	if strings.Contains(text, stem) {
		return 0.5
	}
	return 0
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
