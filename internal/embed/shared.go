package embed

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	once   sync.Once
	shared *Model
	dir    string
	loadEr error
)

// Dir is where the optional model is looked for: $NOAH_EMBED_MODEL, or
// ~/.noah/model. NOAH_EMBED=off disables it.
func Dir() string {
	if p := strings.TrimSpace(os.Getenv("NOAH_EMBED_MODEL")); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".noah", "model")
}

// Shared returns the model found in Dir, loaded once on first use, or nil when
// there is none or it cannot be read. A missing model is normal (the feature is
// optional) and never an error for search.
func Shared() *Model {
	once.Do(func() {
		if strings.EqualFold(strings.TrimSpace(os.Getenv("NOAH_EMBED")), "off") {
			return
		}
		dir = Dir()
		if dir == "" {
			return
		}
		if _, err := os.Stat(filepath.Join(dir, "model.safetensors")); err != nil {
			return
		}
		shared, loadEr = Load(dir)
	})
	return shared
}

// Status describes the optional model for noah doctor: its directory, whether
// it is loaded, and why not when a model file exists but cannot be used.
func Status() (path string, on bool, err error) {
	m := Shared()
	return dir, m != nil, loadEr
}
