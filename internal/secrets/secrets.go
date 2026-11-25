// Package secrets detects credentials in text before it is stored in memory.
//
// Memory is read back into agent context, copied into backups and sometimes
// synced, so a credential saved once keeps leaking. Detection is pattern-based
// and deliberately conservative: it favours catching real secrets over
// perfect precision, and the caller decides whether to redact, block or allow.
package secrets

import (
	"fmt"
	"regexp"
	"strings"
)

// Policy says what to do when a secret is found.
type Policy string

const (
	// Redact replaces each secret with a marker and still saves the memory.
	Redact Policy = "redact"
	// Block refuses to save the memory.
	Block Policy = "block"
	// Off disables scanning.
	Off Policy = "off"
)

// ParsePolicy converts a string to a Policy, defaulting to Redact.
func ParsePolicy(s string) Policy {
	switch Policy(strings.ToLower(strings.TrimSpace(s))) {
	case Block:
		return Block
	case Off:
		return Off
	default:
		return Redact
	}
}

// Finding describes one detected secret. It never contains the secret value.
type Finding struct {
	Kind   string
	Offset int
	Length int
}

type rule struct {
	kind string
	re   *regexp.Regexp
	// group is the capture group holding the secret itself; 0 means the whole match.
	group int
}

var rules = []rule{
	{"private-key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?(?:-----END [A-Z ]*PRIVATE KEY-----|$)`), 0},
	{"aws-access-key", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`), 0},
	{"github-token", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{30,}\b`), 0},
	{"github-fine-grained", regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{40,}\b`), 0},
	{"slack-token", regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}\b`), 0},
	{"anthropic-key", regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}\b`), 0},
	{"openai-style-key", regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{32,}\b`), 0},
	{"stripe-key", regexp.MustCompile(`\b[sr]k_(?:live|test)_[A-Za-z0-9]{16,}\b`), 0},
	{"google-api-key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`), 0},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`), 0},
	// Credentials embedded in a URL: scheme://user:password@host
	{"url-credentials", regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^\s:/@]+:([^\s@/]{3,})@[^\s]+`), 1},
	// key = value assignments with a suspicious name and a non-trivial value.
	{"assigned-secret", regexp.MustCompile(`(?i)\b(?:password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key|auth[_-]?token|client[_-]?secret)["']?\s*[:=]\s*["']?([^\s"',;]{8,})`), 1},
}

// placeholders are values that look like secrets by name but are obviously not.
var placeholders = regexp.MustCompile(`(?i)^(?:x{4,}|\*{4,}|<[^>]+>|\$\{?[A-Z_]+\}?|your[_-].*|changeme|example.*|redacted.*|\[redacted.*|process\.env\..*|env\[.*)$`)

// Scan returns the secrets found in text.
func Scan(text string) []Finding {
	var out []Finding
	for _, r := range rules {
		for _, m := range r.re.FindAllStringSubmatchIndex(text, -1) {
			start, end := m[0], m[1]
			if r.group > 0 && len(m) > 2*r.group+1 && m[2*r.group] >= 0 {
				start, end = m[2*r.group], m[2*r.group+1]
			}
			value := text[start:end]
			if placeholders.MatchString(value) {
				continue
			}
			out = append(out, Finding{Kind: r.kind, Offset: start, Length: end - start})
		}
	}
	return merge(out)
}

// merge drops findings contained in an earlier or larger one.
func merge(in []Finding) []Finding {
	if len(in) < 2 {
		return in
	}
	// insertion sort by offset, then by length descending
	for i := 1; i < len(in); i++ {
		for j := i; j > 0 && less(in[j], in[j-1]); j-- {
			in[j], in[j-1] = in[j-1], in[j]
		}
	}
	out := in[:1]
	for _, f := range in[1:] {
		last := out[len(out)-1]
		if f.Offset < last.Offset+last.Length {
			continue
		}
		out = append(out, f)
	}
	return out
}

func less(a, b Finding) bool {
	if a.Offset != b.Offset {
		return a.Offset < b.Offset
	}
	return a.Length > b.Length
}

// Apply scans text according to policy. It returns the text to store, the
// findings, and an error when the policy is Block and something was found.
func Apply(text string, p Policy) (string, []Finding, error) {
	if p == Off {
		return text, nil, nil
	}
	found := Scan(text)
	if len(found) == 0 {
		return text, nil, nil
	}
	if p == Block {
		return "", found, fmt.Errorf("refusing to save: %d possible secret(s) detected (%s); remove them or use the redact policy", len(found), kinds(found))
	}
	var b strings.Builder
	prev := 0
	for _, f := range found {
		b.WriteString(text[prev:f.Offset])
		b.WriteString("[REDACTED:" + f.Kind + "]")
		prev = f.Offset + f.Length
	}
	b.WriteString(text[prev:])
	return b.String(), found, nil
}

func kinds(fs []Finding) string {
	seen := map[string]bool{}
	var ks []string
	for _, f := range fs {
		if !seen[f.Kind] {
			seen[f.Kind] = true
			ks = append(ks, f.Kind)
		}
	}
	return strings.Join(ks, ", ")
}
