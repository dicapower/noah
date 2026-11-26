// Package embed turns text into small vectors so that search can match
// meaning, not only words ("accessories for my phone" finds a chat about
// screen protectors and power banks).
//
// It runs a Model2Vec static embedding model: a table with one vector per
// word piece. A text is its word pieces, looked up and averaged, so there is no
// neural network to execute, no server, no network and no cgo; a short text
// costs microseconds. The model is an optional local file (model.safetensors,
// tokenizer.json): without it noah behaves exactly as before.
package embed

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// maxWordRunes is BERT's limit: a longer "word" becomes the unknown token.
const maxWordRunes = 100

// Model is a loaded static embedding model. It is safe for concurrent use.
type Model struct {
	vocab map[string]int
	table []float32 // rows of dim floats, one per vocabulary entry
	dim   int
	unk   int
}

// Load reads model.safetensors and tokenizer.json from dir.
func Load(dir string) (*Model, error) {
	tok, err := os.ReadFile(filepath.Join(dir, "tokenizer.json"))
	if err != nil {
		return nil, fmt.Errorf("read tokenizer: %w", err)
	}
	var t struct {
		Model struct {
			Type  string         `json:"type"`
			Unk   string         `json:"unk_token"`
			Vocab map[string]int `json:"vocab"`
		} `json:"model"`
	}
	if err := json.Unmarshal(tok, &t); err != nil {
		return nil, fmt.Errorf("parse tokenizer: %w", err)
	}
	if t.Model.Type != "WordPiece" || len(t.Model.Vocab) == 0 {
		return nil, fmt.Errorf("unsupported tokenizer %q: only WordPiece models are supported", t.Model.Type)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "model.safetensors"))
	if err != nil {
		return nil, fmt.Errorf("read weights: %w", err)
	}
	table, rows, dim, err := readTable(raw)
	if err != nil {
		return nil, err
	}
	if rows < len(t.Model.Vocab) {
		return nil, fmt.Errorf("weights have %d rows for a vocabulary of %d", rows, len(t.Model.Vocab))
	}
	unk, ok := t.Model.Vocab[t.Model.Unk]
	if !ok {
		unk = -1
	}
	return &Model{vocab: t.Model.Vocab, table: table, dim: dim, unk: unk}, nil
}

// readTable parses the first F32 tensor of a safetensors file.
func readTable(b []byte) (table []float32, rows, dim int, err error) {
	if len(b) < 8 {
		return nil, 0, 0, fmt.Errorf("weights file too short")
	}
	n := binary.LittleEndian.Uint64(b[:8])
	if n > uint64(len(b)-8) {
		return nil, 0, 0, fmt.Errorf("weights header is larger than the file")
	}
	var hdr map[string]json.RawMessage
	if err := json.Unmarshal(b[8:8+n], &hdr); err != nil {
		return nil, 0, 0, fmt.Errorf("parse weights header: %w", err)
	}
	data := b[8+n:]
	for name, raw := range hdr {
		if name == "__metadata__" {
			continue
		}
		var info struct {
			DType   string   `json:"dtype"`
			Shape   []int    `json:"shape"`
			Offsets []uint64 `json:"data_offsets"`
		}
		if err := json.Unmarshal(raw, &info); err != nil {
			return nil, 0, 0, fmt.Errorf("parse tensor %s: %w", name, err)
		}
		if info.DType != "F32" || len(info.Shape) != 2 || len(info.Offsets) != 2 {
			return nil, 0, 0, fmt.Errorf("tensor %s: only 2-D F32 tables are supported", name)
		}
		lo, hi := info.Offsets[0], info.Offsets[1]
		rows, dim = info.Shape[0], info.Shape[1]
		if hi > uint64(len(data)) || lo > hi || hi-lo != uint64(rows)*uint64(dim)*4 {
			return nil, 0, 0, fmt.Errorf("tensor %s: offsets do not match its shape", name)
		}
		table = make([]float32, rows*dim)
		for i := range table {
			table[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[lo+uint64(i)*4:]))
		}
		return table, rows, dim, nil
	}
	return nil, 0, 0, fmt.Errorf("no tensor found in weights")
}

// Dim is the length of every vector.
func (m *Model) Dim() int { return m.dim }

// Embed returns the unit-length vector of text, or nil when the text has no
// known word pieces.
func (m *Model) Embed(text string) []float32 {
	sum := make([]float32, m.dim)
	n := 0
	for _, w := range pretokenize(text) {
		for _, id := range m.wordPieces(w) {
			if id == m.unk {
				continue
			}
			row := m.table[id*m.dim : (id+1)*m.dim]
			for i, v := range row {
				sum[i] += v
			}
			n++
		}
	}
	if n == 0 {
		return nil
	}
	var norm2 float64
	for _, v := range sum {
		norm2 += float64(v) * float64(v)
	}
	if norm2 == 0 {
		return nil
	}
	inv := float32(1 / math.Sqrt(norm2))
	for i := range sum {
		sum[i] *= inv
	}
	return sum
}

// Cosine is the dot product of two unit vectors.
func Cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var s float64
	for i := range a {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}

// wordPieces splits one word greedily into the longest known pieces; a word
// that cannot be covered is a single unknown token.
func (m *Model) wordPieces(w string) []int {
	r := []rune(w)
	if len(r) > maxWordRunes {
		return []int{m.unk}
	}
	var out []int
	for start := 0; start < len(r); {
		end, found := len(r), -1
		for ; end > start; end-- {
			p := string(r[start:end])
			if start > 0 {
				p = "##" + p
			}
			if id, ok := m.vocab[p]; ok {
				found = id
				break
			}
		}
		if found < 0 {
			return []int{m.unk}
		}
		out = append(out, found)
		start = end
	}
	return out
}

// pretokenize follows BERT: lowercase, strip accents, drop control characters,
// split on white space and make every punctuation mark and CJK character its
// own word.
func pretokenize(text string) []string {
	// A transformer keeps state, so each call builds its own: Model is shared.
	s, _, err := transform.String(transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC), strings.ToLower(text))
	if err != nil {
		s = strings.ToLower(text)
	}
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, string(cur))
			cur = cur[:0]
		}
	}
	for _, r := range s {
		switch {
		case r == 0 || r == 0xFFFD || (unicode.IsControl(r) && !unicode.IsSpace(r)):
			// dropped
		case unicode.IsSpace(r):
			flush()
		case isPunct(r) || unicode.Is(unicode.Han, r):
			flush()
			words = append(words, string(r))
		default:
			cur = append(cur, r)
		}
	}
	flush()
	return words
}

func isPunct(r rune) bool {
	if (r >= 33 && r <= 47) || (r >= 58 && r <= 64) || (r >= 91 && r <= 96) || (r >= 123 && r <= 126) {
		return true
	}
	return unicode.IsPunct(r)
}
