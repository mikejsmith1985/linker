package persona

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultNonEmpty(t *testing.T) {
	if strings.TrimSpace(Default()) == "" {
		t.Fatal("Default() is empty")
	}
	if !strings.Contains(Default(), "HASHTAGS:") {
		t.Error("default prompt missing HASHTAGS output contract")
	}
}

func TestLoadEmptyPathReturnsDefault(t *testing.T) {
	got, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\"): %v", err)
	}
	if got != Default() {
		t.Error("Load(\"\") did not return the embedded default")
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "voice.md")
	if err := os.WriteFile(p, []byte("custom voice"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != "custom voice" {
		t.Errorf("got %q, want %q", got, "custom voice")
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.md")); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadEmptyFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "empty.md")
	if err := os.WriteFile(p, []byte("   \n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Load(p); err == nil {
		t.Error("expected error for empty file")
	}
}
