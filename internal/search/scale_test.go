package search_test

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dicapower/noah/internal/memory"
	"github.com/dicapower/noah/internal/search"
	"github.com/dicapower/noah/internal/store"
)

// TestScale measures insert rate, size and search latency/recall on synthetic
// data. It only runs when NOAH_SCALE is set, e.g. NOAH_SCALE=1000,10000,50000.
func TestScale(t *testing.T) {
	spec := os.Getenv("NOAH_SCALE")
	if spec == "" {
		t.Skip("set NOAH_SCALE=1000,10000 to run")
	}
	var sizes []int
	for _, f := range strings.Split(spec, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil {
			t.Fatal(err)
		}
		sizes = append(sizes, n)
	}
	fmt.Printf("\n%-8s %-12s %-8s | %-28s %-28s %-28s | %s\n", "N", "insert/s", "db MB", "common term p50/p95 ms", "3 terms p50/p95 ms", "short term p50/p95 ms", "NL question recall (top1/top5 of 50)")
	for _, n := range sizes {
		runScale(t, n)
	}
}

func words() []string {
	r := rand.New(rand.NewSource(7))
	syll := []string{"ka", "lo", "mi", "ren", "tor", "vex", "pla", "sun", "dor", "fin", "gar", "hel", "jun", "mor", "nas", "qui", "bru", "sol", "tan", "wex"}
	seen := map[string]bool{}
	var w []string
	for len(w) < 3000 {
		k := 1 + r.Intn(3)
		var b strings.Builder
		for i := 0; i < k+1; i++ {
			b.WriteString(syll[r.Intn(len(syll))])
		}
		if s := b.String(); !seen[s] {
			seen[s] = true
			w = append(w, s)
		}
	}
	return w
}

func runScale(t *testing.T, n int) {
	vocab := words()
	r := rand.New(rand.NewSource(42))
	zipf := rand.NewZipf(r, 1.2, 4, uint64(len(vocab)-1))
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "scale.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	planted := map[int]int64{} // planted index -> memory id
	stepEvery := n / 50
	start := time.Now()
	for i := 0; i < n; i++ {
		var b strings.Builder
		for b.Len() < 900 {
			b.WriteString(vocab[zipf.Uint64()])
			b.WriteByte(' ')
		}
		content := b.String()
		k := -1
		if stepEvery > 0 && i%stepEvery == 0 && len(planted) < 50 {
			k = len(planted)
			content = fmt.Sprintf("We fixed the zorblax%d outage caused by flimmer%d. ", k, k) + content
		}
		res, err := s.Save(ctx, memory.Input{Space: "bench", Title: fmt.Sprintf("note %d", i), Content: content, Kind: "note"})
		if err != nil {
			t.Fatal(err)
		}
		if k >= 0 {
			planted[k] = res.Memory.ID
		}
	}
	insertRate := float64(n) / time.Since(start).Seconds()
	fi, _ := os.Stat(filepath.Join(dir, "scale.db"))
	sizeMB := float64(fi.Size()) / 1e6

	measure := func(q string) (p50, p95 float64) {
		var d []float64
		for i := 0; i < 100; i++ {
			t0 := time.Now()
			if _, err := search.Run(ctx, s, q, search.Options{Space: "bench"}); err != nil {
				t.Fatal(err)
			}
			d = append(d, float64(time.Since(t0).Microseconds())/1000)
		}
		sort.Float64s(d)
		return d[len(d)/2], d[int(math.Ceil(0.95*float64(len(d))))-1]
	}
	c50, c95 := measure(vocab[0])
	m50, m95 := measure(strings.Join(vocab[20:23], " "))
	s50, s95 := measure(vocab[1][:3])

	top1, top5 := 0, 0
	for k := 0; k < len(planted); k++ {
		hits, err := search.Run(ctx, s, fmt.Sprintf("how did we handle the zorblax%d issue with flimmer%d?", k, k), search.Options{Space: "bench"})
		if err != nil {
			t.Fatal(err)
		}
		for rank, h := range hits {
			if h.Memory.ID == planted[k] {
				if rank == 0 {
					top1++
				}
				if rank < 5 {
					top5++
				}
				break
			}
		}
	}
	fmt.Printf("%-8d %-12.0f %-8.1f | %-28s %-28s %-28s | %d/%d\n", n, insertRate, sizeMB,
		fmt.Sprintf("%.1f / %.1f", c50, c95), fmt.Sprintf("%.1f / %.1f", m50, m95), fmt.Sprintf("%.1f / %.1f", s50, s95), top1, top5)
}
