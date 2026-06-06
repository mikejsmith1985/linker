// Package persona supplies the system prompt that defines the LinkedIn voice
// linker writes in. A sensible default is embedded; users can override it by
// pointing PERSONA_PROMPT_PATH at their own markdown file.
package persona

import (
	_ "embed"
	"fmt"
	"os"
	"strings"
)

//go:embed persona.md
var defaultPrompt string

// Default returns the built-in persona prompt.
func Default() string { return defaultPrompt }

// Load returns the persona prompt. When path is empty the embedded default is
// used; otherwise the file at path is read. A path that is set but unreadable
// (or empty) is treated as an error so misconfiguration is surfaced loudly
// rather than silently falling back.
func Load(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return defaultPrompt, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read persona prompt %q: %w", path, err)
	}
	if strings.TrimSpace(string(b)) == "" {
		return "", fmt.Errorf("persona prompt %q is empty", path)
	}
	return string(b), nil
}
