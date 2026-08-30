package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIConfigControlsDefaultHarnesses(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CMS_CONFIG_DIR", filepath.Join(root, "config"))
	t.Setenv("CMS_DATA_DIR", filepath.Join(root, "data"))
	t.Setenv("CMS_CACHE_DIR", filepath.Join(root, "cache"))

	var out, errOut bytes.Buffer
	if code := Run([]string{"config", "set", "default-targets", "codex,cursor"}, &out, &errOut, strings.NewReader("")); code != 0 {
		t.Fatalf("config set: code=%d err=%s", code, errOut.String())
	}
	app, err := New(&out, &errOut, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	if got := app.defaultTargetNames(); !equalStrings(got, []string{"codex", "cursor"}) {
		t.Fatalf("effective targets = %#v", got)
	}

	out.Reset()
	errOut.Reset()
	if code := Run([]string{"config", "unset", "default-targets"}, &out, &errOut, strings.NewReader("")); code != 0 {
		t.Fatalf("config unset: code=%d err=%s", code, errOut.String())
	}
	app, err = New(&out, &errOut, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	if got := app.defaultTargetNames(); !equalStrings(got, []string{"antigravity", "claude", "codex", "cursor", "opencode"}) {
		t.Fatalf("all effective targets = %#v", got)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
