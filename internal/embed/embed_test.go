package embed

import (
	"math"
	"os"
	"reflect"
	"testing"
)

func TestPretokenize(t *testing.T) {
	got := pretokenize("Café, déjà-vu!  3.14\tOK")
	want := []string{"cafe", ",", "deja", "-", "vu", "!", "3", ".", "14", "ok"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestWordPiecesGreedy(t *testing.T) {
	m := &Model{vocab: map[string]int{"[UNK]": 0, "un": 1, "##believ": 2, "##able": 3, "play": 4}, unk: 0}
	if got := m.wordPieces("unbelievable"); !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Fatalf("pieces: %v", got)
	}
	if got := m.wordPieces("zzz"); !reflect.DeepEqual(got, []int{0}) {
		t.Fatalf("unknown word should be [UNK]: %v", got)
	}
}

func TestReadTableRejectsBadFiles(t *testing.T) {
	for _, b := range [][]byte{nil, {1, 2, 3}, append([]byte{255, 255, 255, 255, 255, 255, 255, 127}, 0)} {
		if _, _, _, err := readTable(b); err == nil {
			t.Errorf("accepted %v", b)
		}
	}
}

// With a real model on disk (NOAH_EMBED_MODEL), related texts must be closer
// than unrelated ones and vectors must have unit length.
func TestRealModelSanity(t *testing.T) {
	dir := os.Getenv("NOAH_EMBED_MODEL")
	if dir == "" {
		t.Skip("set NOAH_EMBED_MODEL to a Model2Vec directory")
	}
	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := m.Embed("useful accessories for my phone")
	b := m.Embed("screen protector and wireless charging power bank for iPhone")
	c := m.Embed("how to bake sourdough bread")
	if math.Abs(Cosine(a, a)-1) > 1e-4 {
		t.Fatalf("not unit length: %v", Cosine(a, a))
	}
	if Cosine(a, b) <= Cosine(a, c) {
		t.Fatalf("phone accessories should be closer to phone gear (%v) than to bread (%v)", Cosine(a, b), Cosine(a, c))
	}
	if m.Embed("") != nil {
		t.Fatal("empty text must have no vector")
	}
}

func TestPretokenizeIsSafeForConcurrentUse(t *testing.T) {
	done := make(chan bool)
	for i := 0; i < 8; i++ {
		go func() {
			for j := 0; j < 200; j++ {
				pretokenize("Café déjà-vu, naïve résumé!")
			}
			done <- true
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
}
