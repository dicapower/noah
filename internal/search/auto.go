package search

import "github.com/dicapower/noah/internal/embed"

// Auto returns the optional local embedding model, or nil when none is
// installed. It exists so that callers never hold a typed nil in an interface.
func Auto() Embedder {
	if m := embed.Shared(); m != nil {
		return m
	}
	return nil
}
