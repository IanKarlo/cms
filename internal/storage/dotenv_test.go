package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("# comment\nOTHER=value\nexport GITHUB_TOKEN=\"github_pat_example\" # local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := LoadDotEnvValue(path, "GITHUB_TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	if value != "github_pat_example" {
		t.Fatalf("unexpected dotenv value: %q", value)
	}
}

func TestLoadDotEnvValueMissingFile(t *testing.T) {
	value, err := LoadDotEnvValue(filepath.Join(t.TempDir(), ".env"), "GITHUB_TOKEN")
	if err != nil || value != "" {
		t.Fatalf("missing dotenv should be ignored: %q %v", value, err)
	}
}
