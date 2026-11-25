package search_test

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dicapower/noah/internal/memory"
	"github.com/dicapower/noah/internal/search"
	"github.com/dicapower/noah/internal/store"
)

// The recall test plants 40 "needle" memories among hard negatives and filler,
// then asks natural-language questions about them. It guards the quality of
// search: a ranking change that makes noah miss an answer fails here.
//
// The corpus is generated from a fixed seed, so every run sees the same data.
// By default it uses 2,000 memories to stay fast; NOAH_RECALL_N=10000 runs the
// larger case that bench/ also measures through the CLI.

var common = strings.Fields(`servicio datos equipo usuario despliegue configuración error cliente token cache cola test release rama
módulo producción staging logs métrica alerta límite pool conexiones versión migración índice consulta
respuesta permisos rol correo pago factura pedido informe service database deploy config queue retry
timeout cluster worker schema index backend frontend pipeline build commit review ticket sprint
nodo clave sesión proyecto repositorio contenedor imagen puerto dominio certificado copia restauración`)

type corpus struct {
	rows      []row
	questions []question
}

type row struct{ title, content, kind string }

type question struct {
	kind   string // "full" or "partial"
	needle string // title prefix of the memory that answers it
	text   string
}

func buildCorpus(n int) corpus {
	rng := rand.New(rand.NewSource(42))
	syl := strings.Fields("ka ro mi tu ve lo sa ne di po xu qe zi bo fa gu he jo ly mo")
	used := map[string]bool{}
	for _, w := range common {
		used[w] = true
	}
	pseudo := func() string {
		for {
			var b strings.Builder
			for i, k := 0, 3+rng.Intn(2); i < k; i++ {
				b.WriteString(syl[rng.Intn(len(syl))])
			}
			if w := b.String(); !used[w] {
				used[w] = true
				return w
			}
		}
	}
	rare := make([]string, 4000)
	for i := range rare {
		rare[i] = pseudo()
	}
	entities := make([]string, 200)
	for i := range entities {
		w := pseudo()
		entities[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	zc := rand.NewZipf(rng, 1.1, 1, uint64(len(common)-1))
	zr := rand.NewZipf(rng, 1.1, 1, uint64(len(rare)-1))
	filler := func(words int) string {
		var b strings.Builder
		for i := 0; i < words; i++ {
			if rng.Float64() < 0.55 {
				b.WriteString(common[zc.Uint64()])
			} else {
				b.WriteString(rare[zr.Uint64()])
			}
			b.WriteByte(' ')
		}
		return strings.TrimSpace(b.String()) + "."
	}
	mid := common[12:]
	pickRare := func() string { return rare[1500+rng.Intn(len(rare)-1500)] }

	var c corpus
	var head []row
	for i := 0; i < 40; i++ {
		t1, t2 := mid[rng.Intn(len(mid))], mid[rng.Intn(len(mid))]
		for t2 == t1 {
			t2 = mid[rng.Intn(len(mid))]
		}
		ent := entities[i]
		d1, d2 := pickRare(), pickRare()
		code := fmt.Sprintf("Needle-%03d", i)
		head = append(head, row{code + " " + t1 + " " + t2 + " " + ent, fmt.Sprintf("Decidimos %s y %s para %s: %s %s. ", t1, t2, ent, d1, d2) + filler(60), "decision"})
		other := entities[100+i]
		head = append(head,
			row{fmt.Sprintf("Distractor-%03da %s %s %s", i, t1, t2, other), fmt.Sprintf("Decidimos %s y %s para %s: %s. ", t1, t2, other, pickRare()) + filler(60), "decision"},
			row{fmt.Sprintf("Distractor-%03db %s %s", i, mid[rng.Intn(len(mid))], ent), fmt.Sprintf("Notas sobre %s: %s. ", ent, pickRare()) + filler(60), "decision"},
			row{fmt.Sprintf("Distractor-%03dc %s %s", i, t1, mid[rng.Intn(len(mid))]), fmt.Sprintf("Sobre %s: %s. ", t1, pickRare()) + filler(60), "decision"},
		)
		if i < 20 {
			c.questions = append(c.questions, question{"full", code, fmt.Sprintf("¿Qué decidimos sobre %s %s de %s con %s?", t1, t2, ent, d1)})
		} else {
			c.questions = append(c.questions, question{"partial", code, fmt.Sprintf("cómo configuramos %s para %s en producción", ent, t1)})
		}
	}
	note := func(i int) row {
		return row{fmt.Sprintf("Note-%05d %s %s %s %s", i, common[rng.Intn(len(common))], common[rng.Intn(len(common))], common[rng.Intn(len(common))], rare[rng.Intn(len(rare))]), filler(130), ""}
	}
	for len(head) < 1000 {
		head = append(head, note(len(head)))
	}
	rng.Shuffle(len(head), func(i, j int) { head[i], head[j] = head[j], head[i] })
	c.rows = head
	for len(c.rows) < n {
		c.rows = append(c.rows, note(len(c.rows)))
	}
	return c
}

type recallResult struct{ fullTop1, fullTop5, partTop1, partTop5, fullN, partN int }

func measureRecall(t *testing.T, n int) recallResult {
	t.Helper()
	c := buildCorpus(n)
	s, err := store.Open(filepath.Join(t.TempDir(), "recall.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	for _, r := range c.rows {
		if _, err := s.Save(ctx, memory.Input{Space: "bench", Title: r.title, Content: r.content, Kind: r.kind}); err != nil {
			t.Fatal(err)
		}
	}
	var res recallResult
	for _, q := range c.questions {
		hits, err := search.Run(ctx, s, q.text, search.Options{Space: "bench", Limit: 8})
		if err != nil {
			t.Fatal(err)
		}
		top1, top5 := false, false
		for i, h := range hits {
			if strings.HasPrefix(h.Memory.Title, q.needle+" ") {
				top1 = top1 || i == 0
				top5 = top5 || i < 5
			}
		}
		if q.kind == "full" {
			res.fullN++
			res.fullTop1 += b2i(top1)
			res.fullTop5 += b2i(top5)
		} else {
			res.partN++
			res.partTop1 += b2i(top1)
			res.partTop5 += b2i(top5)
		}
	}
	return res
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestRecall(t *testing.T) {
	n := 2000
	if v, err := strconv.Atoi(os.Getenv("NOAH_RECALL_N")); err == nil && v >= 1000 {
		n = v
	}
	r := measureRecall(t, n)
	t.Logf("N=%d  full: top1 %d/%d top5 %d/%d   partial: top1 %d/%d top5 %d/%d",
		n, r.fullTop1, r.fullN, r.fullTop5, r.fullN, r.partTop1, r.partN, r.partTop5, r.partN)
	if os.Getenv("NOAH_RECALL_REPORT") != "" {
		return // measuring, not asserting
	}
	if r.fullTop5 < r.fullN {
		t.Errorf("questions with nearly all the needle's terms: %d/%d in the top 5, want all", r.fullTop5, r.fullN)
	}
	if r.partTop5 < minPartial {
		t.Errorf("partial questions: %d/%d in the top 5, want at least %d", r.partTop5, r.partN, minPartial)
	}
}
