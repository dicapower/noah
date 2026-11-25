// Package memory defines what a stored memory is and the rules every memory
// must satisfy before it reaches the database.
package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Trust says who vouches for a memory. It is shown next to the memory when it
// is injected into an agent's context, and it protects user-written memories
// from being overwritten by an agent.
type Trust string

const (
	// TrustUser is written by a human (CLI) and cannot be overwritten by agents.
	TrustUser Trust = "user"
	// TrustAgent is written by an AI agent through MCP.
	TrustAgent Trust = "agent"
	// TrustExternal comes from outside content (web pages, files, tool output).
	TrustExternal Trust = "external"
)

// ParseTrust parses a trust level; empty means TrustAgent.
func ParseTrust(s string) (Trust, error) {
	switch Trust(strings.ToLower(strings.TrimSpace(s))) {
	case "", TrustAgent:
		return TrustAgent, nil
	case TrustUser:
		return TrustUser, nil
	case TrustExternal:
		return TrustExternal, nil
	}
	return "", fmt.Errorf("%w: trust must be user, agent or external", ErrInvalid)
}

// Rank is a 0..1 weight used when ranking search results.
func (t Trust) Rank() float64 {
	switch t {
	case TrustUser:
		return 1
	case TrustAgent:
		return 0.6
	}
	return 0.2
}

// Limits keep one memory from bloating the database or the agent's context.
const (
	MaxTitle   = 200
	MaxContent = 64 * 1024
	MaxTags    = 20
	MaxTagLen  = 40
	MaxTopic   = 120
)

// ErrInvalid wraps every validation failure.
var ErrInvalid = errors.New("invalid memory")

// Memory is a stored memory.
type Memory struct {
	ID         int64    `json:"id"`
	Space      string   `json:"space"`
	Title      string   `json:"title"`
	Content    string   `json:"content"`
	Kind       string   `json:"kind"`
	Topic      string   `json:"topic,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Trust      Trust    `json:"trust"`
	Pinned     bool     `json:"pinned,omitempty"`
	Revision   int      `json:"revision"`
	Redactions int      `json:"redactions,omitempty"`
	CreatedAt  int64    `json:"created_at"` // unix milliseconds
	UpdatedAt  int64    `json:"updated_at"` // unix milliseconds
}

// Scored is a search candidate: a memory and its BM25 score (lower is better,
// as returned by SQLite FTS5).
type Scored struct {
	Memory Memory
	BM25   float64
}

// Input is what a caller supplies to save a memory.
type Input struct {
	Space   string
	Title   string
	Content string
	Kind    string
	Topic   string
	Tags    []string
	Trust   Trust
	Pinned  bool

	// CreatedAt and UpdatedAt are optional; they let importers keep original dates.
	CreatedAt, UpdatedAt time.Time
}

var (
	kindRe  = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	topicRe = regexp.MustCompile(`^[a-z0-9][a-z0-9/_.:-]*$`)
	tagRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9_.:-]*$`)
)

// Clean validates and normalises an Input. It never changes the meaning of the
// content; it only trims, lowercases identifiers and fills defaults.
func (in Input) Clean() (Input, error) {
	bad := func(format string, a ...any) (Input, error) {
		return Input{}, fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
	}
	in.Content = strings.TrimSpace(in.Content)
	if in.Content == "" {
		return bad("content is empty")
	}
	if len(in.Content) > MaxContent {
		return bad("content is %d bytes, the limit is %d; split it into several memories", len(in.Content), MaxContent)
	}
	if !utf8.ValidString(in.Content) || strings.ContainsRune(in.Content, 0) {
		return bad("content must be valid UTF-8 text")
	}
	// Title and Kind may stay empty: the store derives a title / defaults the
	// kind when creating, and keeps the existing values when revising a topic.
	in.Title = strings.TrimSpace(strings.ReplaceAll(in.Title, "\n", " "))
	if utf8.RuneCountInString(in.Title) > MaxTitle {
		return bad("title is longer than %d characters", MaxTitle)
	}
	in.Kind = strings.ToLower(strings.TrimSpace(in.Kind))
	if in.Kind != "" && !kindRe.MatchString(in.Kind) {
		return bad("kind %q must be lowercase letters, digits, - or _ (max 32)", in.Kind)
	}
	in.Topic = strings.ToLower(strings.Join(strings.Fields(in.Topic), "-"))
	if in.Topic != "" && (len(in.Topic) > MaxTopic || !topicRe.MatchString(in.Topic)) {
		return bad("topic %q must be lowercase letters, digits and / _ . : - (max %d)", in.Topic, MaxTopic)
	}
	tags, err := cleanTags(in.Tags)
	if err != nil {
		return Input{}, err
	}
	in.Tags = tags
	if in.Trust == "" {
		in.Trust = TrustAgent
	}
	space, err := NormalizeSpace(in.Space)
	if err != nil {
		return Input{}, err
	}
	in.Space = space
	return in, nil
}

func cleanTags(in []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] {
			continue
		}
		if len(t) > MaxTagLen || !tagRe.MatchString(t) {
			return nil, fmt.Errorf("%w: tag %q must be lowercase letters, digits and _ . : - (max %d)", ErrInvalid, t, MaxTagLen)
		}
		seen[t] = true
		out = append(out, t)
	}
	if len(out) > MaxTags {
		return nil, fmt.Errorf("%w: at most %d tags", ErrInvalid, MaxTags)
	}
	return out, nil
}

// DefaultKind is the kind given to a new memory that does not name one.
const DefaultKind = "note"

// DeriveTitle makes a title from the first line of content.
func DeriveTitle(content string) string {
	line := strings.TrimSpace(strings.SplitN(content, "\n", 2)[0])
	r := []rune(line)
	if len(r) > 80 {
		return string(r[:80]) + "…"
	}
	return line
}

// Hash identifies the content of a memory for duplicate detection. Case and
// whitespace differences do not make two memories distinct.
func Hash(content string) string {
	norm := strings.ToLower(strings.Join(strings.Fields(content), " "))
	sum := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(sum[:])
}
