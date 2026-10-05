package notectl

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteBack(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	day := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)

	// notectl not configured → silent no-op, nothing written anywhere
	if err := WriteBack(day, "body"); err != nil {
		t.Fatalf("unconfigured WriteBack = %v, want nil", err)
	}

	vault := filepath.Join(home, "vault")
	cfgDir := filepath.Join(home, ".config", "notectl")
	_ = os.MkdirAll(cfgDir, 0o755)
	if err := os.WriteFile(filepath.Join(cfgDir, "notectl.yaml"), []byte("vault_path: ~/vault\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteBack(day, "# entry"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(vault, "Diary", "2026-10-05.md"))
	if err != nil || string(got) != "# entry" {
		t.Errorf("written file = %q, %v ('~' in vault_path must expand)", got, err)
	}
	// rewriting the same day replaces the file
	_ = WriteBack(day, "v2")
	if got, _ = os.ReadFile(filepath.Join(vault, "Diary", "2026-10-05.md")); string(got) != "v2" {
		t.Errorf("rewrite = %q", got)
	}
}

func TestWriteBackBrokenConfigIsSilent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgDir := filepath.Join(home, ".config", "notectl")
	_ = os.MkdirAll(cfgDir, 0o755)
	_ = os.WriteFile(filepath.Join(cfgDir, "notectl.yaml"), []byte(":\n\t- not yaml {{"), 0o644)
	if err := WriteBack(time.Now(), "x"); err != nil {
		t.Errorf("corrupt notectl config must not fail the diary: %v", err)
	}
}
