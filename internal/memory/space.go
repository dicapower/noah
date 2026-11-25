package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Unscoped is the space used when no project can be identified, for example
// when an agent runs in the home directory. Keeping it separate stops memories
// from unrelated work piling up under one project name.
const Unscoped = "_unscoped"

var (
	spaceRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	spaceJunk = regexp.MustCompile(`[^a-z0-9._-]+`)
)

// NormalizeSpace turns a name into a valid space id. Empty input maps to Unscoped.
func NormalizeSpace(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == Unscoped {
		return Unscoped, nil
	}
	s = strings.Trim(spaceJunk.ReplaceAllString(s, "-"), "-.")
	if len(s) > 64 {
		s = s[:64]
	}
	if !spaceRe.MatchString(s) {
		return "", fmt.Errorf("%w: space name has no usable characters", ErrInvalid)
	}
	return s, nil
}

// ResolveSpace decides which space cwd belongs to. In order: the NOAH_SPACE
// environment variable; the nearest ".noah" file (its first line is the name)
// or ".git" entry found walking up from cwd; the name of cwd itself. The home
// directory and the filesystem root resolve to Unscoped.
func ResolveSpace(cwd string) string {
	if v := os.Getenv("NOAH_SPACE"); strings.TrimSpace(v) != "" {
		if s, err := NormalizeSpace(v); err == nil {
			return s
		}
	}
	if cwd == "" {
		return Unscoped
	}
	cwd = filepath.Clean(cwd)
	home, _ := os.UserHomeDir()
	if home != "" {
		home = filepath.Clean(home)
	}
	if cwd == home || cwd == string(filepath.Separator) {
		return Unscoped
	}
	for dir := cwd; ; dir = filepath.Dir(dir) {
		if dir == home && dir != cwd {
			break
		}
		if b, err := os.ReadFile(filepath.Join(dir, ".noah")); err == nil {
			for _, line := range strings.Split(string(b), "\n") {
				if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
					if s, err := NormalizeSpace(line); err == nil {
						return s
					}
					break
				}
			}
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return nameOf(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return nameOf(cwd)
}

func nameOf(dir string) string {
	s, err := NormalizeSpace(filepath.Base(dir))
	if err != nil {
		return Unscoped
	}
	return s
}
