package storage

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestConfigDefaultTargetsUseAllWhenUnset(t *testing.T) {
	p := Paths{ConfigDir: filepath.Join(t.TempDir(), "config")}
	c, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.DefaultTargets) != 0 {
		t.Fatalf("default targets = %#v, want unset", c.DefaultTargets)
	}
}

func TestSaveDefaultTargetsPreservesOtherConfig(t *testing.T) {
	p := Paths{ConfigDir: filepath.Join(t.TempDir(), "config")}
	if err := EnsureDirs(p); err != nil {
		t.Fatal(err)
	}
	original := "# keep this comment\nschema_version = 1\ndefault_targets = [\"claude\"]\n\n[github]\nsearch_limit = 42\n\n[mcp_registry]\ncache_ttl = \"1h\"\n"
	if err := os.WriteFile(p.ConfigFile(), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveDefaultTargets(p, []string{"codex", "cursor"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	content := string(b)
	if !strings.Contains(content, "# keep this comment") || !strings.Contains(content, "search_limit = 42") || !strings.Contains(content, "cache_ttl = \"1h\"") {
		t.Fatalf("unrelated config was changed: %s", content)
	}
	c, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.DefaultTargets, []string{"codex", "cursor"}) {
		t.Fatalf("default targets = %#v", c.DefaultTargets)
	}
	if err := SaveDefaultTargets(p, nil); err != nil {
		t.Fatal(err)
	}
	c, err = LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.DefaultTargets) != 0 || strings.Contains(string(mustRead(t, p.ConfigFile())), "default_targets") {
		t.Fatalf("default targets were not cleared: %#v", c.DefaultTargets)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
