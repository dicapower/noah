package memory

import (
	"regexp"
	"strings"
	"unicode"
)

// Flags raised by Suspicious. They are stored with the memory and shown next
// to it, so the agent (and the person reading the output) can tell a note that
// merely records a fact from one that tries to give orders.
const (
	FlagInstruction = "instruction" // addresses the AI with commands or tries to override earlier rules
	FlagCommand     = "command"     // contains a shell one-liner that downloads, decodes or deletes
	FlagHidden      = "hidden"      // contains invisible or direction-changing characters
)

// A bare link is not a flag: nearly every real note about a PR or ticket has
// one, and flagging them hid a user's recent notes from automatic context. A
// link only matters together with an instruction, which is flagged on its own.
// "Without asking" is not a flag either: it mostly appears in protective rules
// ("never delete X without asking").
//
// Persistent memory is an injection target: one poisoned write is replayed in
// every later session. Detection here is deliberately cheap and heuristic. It
// does not stop a determined attacker; it makes the common cases visible and
// keeps them out of automatic injection (see Injectable).
var (
	instructionRe = regexp.MustCompile(`(?i)` +
		`ignore (all |any |the )?(previous|prior|above|earlier) (instructions|rules|messages)|` +
		`disregard (all |any |the )?(previous|prior|above|earlier)|` +
		`ignora(r)? (todas )?(las |tus )?(instrucciones|reglas) (anteriores|previas)|` +
		`olvida (todas )?(las |tus )?(instrucciones|reglas)|` +
		`(you|tu) (must|should|have to|debes|tienes que) (always |now |siempre |ahora )?(run|execute|ejecut|send|env[ií]a|upload|sube|delete|borra|curl|fetch)|` +
		`(always|siempre|from now on|a partir de ahora) (send|env[ií]a|upload|sube|ignore|ignora|obey|obedece)|` +
		`do not (tell|inform|mention to) the user|no (le )?(digas|cuentes|avises) al usuario|` +
		`new instructions|nuevas instrucciones|` +
		`(reveal|print|show|muestra|revela|imprime) (your |the |tu |el )?(system|hidden|secret|api|token|clave|secreto)`)
	commandRe = regexp.MustCompile(`(?i)(curl|wget)[^\n|]*\|\s*(sudo\s+)?(ba|z|da)?sh\b|base64\s+(-d|--decode)|rm\s+-[a-z]*r[a-z]*f?\s+(/|~|\$HOME)|chmod\s+\+x|eval\s*\(|powershell[^\n]*-enc`)
)

// Suspicious returns the flags that apply to a memory's title and content.
func Suspicious(title, content string) []string {
	text := title + "\n" + content
	var out []string
	if instructionRe.MatchString(text) {
		out = append(out, FlagInstruction)
	}
	if commandRe.MatchString(text) {
		out = append(out, FlagCommand)
	}
	if hasInvisible(text) {
		out = append(out, FlagHidden)
	}
	return out
}

// Injectable reports whether a memory may be placed in the agent's context
// automatically (session start, mem_context). Flagged notes written by the
// agent or by an external source are held back: they stay searchable, with a
// warning, but nothing replays them into a session on its own. A note the
// person wrote or confirmed (trust "user") is always injectable.
func Injectable(m Memory) bool {
	return len(m.Flags) == 0 || m.Trust == TrustUser
}

// hasInvisible reports characters that render as nothing or reorder text:
// zero-width spaces and joiners, bidirectional controls, the BOM, and control
// characters other than tab and newline.
func hasInvisible(s string) bool {
	for _, r := range s {
		if isInvisible(r) {
			return true
		}
	}
	return false
}

func isInvisible(r rune) bool {
	switch {
	case r >= 0x200B && r <= 0x200F, // zero-width space/joiners, LRM, RLM
		r >= 0x202A && r <= 0x202E, // bidi embeddings and overrides
		r >= 0x2060 && r <= 0x2064, // word joiner, invisible operators
		r >= 0x2066 && r <= 0x2069, // bidi isolates
		r == 0xFEFF:
		return true
	case r == '\t' || r == '\n' || r == '\r':
		return false
	}
	return unicode.IsControl(r)
}

// StripInvisible removes the characters hasInvisible looks for.
func StripInvisible(s string) string {
	if !hasInvisible(s) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if isInvisible(r) {
			return -1
		}
		return r
	}, s)
}

// EstimateTokens approximates how many tokens a text costs an LLM. Spanish and
// English prose average about 3.5 characters per token; this is an estimate
// (about ±20%), meant for budgets, not billing.
func EstimateTokens(s string) int {
	return (len([]rune(s))*2 + 6) / 7
}

// CharsForTokens is the inverse of EstimateTokens.
func CharsForTokens(t int) int { return t * 7 / 2 }
