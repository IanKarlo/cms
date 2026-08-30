package storage

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ikts/cms/internal/model"
)

// MCPRegistry stores definitions only. It deliberately has no operation that
// starts a process or contacts a remote server.
type MCPRegistry struct{ Paths Paths }

func (r MCPRegistry) Path(id string) string { return filepath.Join(r.Paths.MCPDir(), id+".toml") }

func (r MCPRegistry) Get(id string) (model.MCPMetadata, error) {
	b, err := os.ReadFile(r.Path(id))
	if err != nil {
		return model.MCPMetadata{}, fmt.Errorf("MCP %q not found", id)
	}
	d, err := parseDocument(string(b))
	if err != nil {
		return model.MCPMetadata{}, err
	}
	m, err := parseMCPDocument(d)
	if err != nil {
		return model.MCPMetadata{}, err
	}
	if m.ID != id {
		return model.MCPMetadata{}, fmt.Errorf("MCP registry entry %q has mismatched ID", id)
	}
	return m, nil
}

func (r MCPRegistry) List() ([]model.MCPMetadata, error) {
	entries, err := os.ReadDir(r.Paths.MCPDir())
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	var out []model.MCPMetadata
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		m, getErr := r.Get(strings.TrimSuffix(e.Name(), ".toml"))
		if getErr != nil {
			return nil, getErr
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (r MCPRegistry) Save(m model.MCPMetadata) error {
	if err := ValidateMCP(m); err != nil {
		return err
	}
	if m.ID == "" {
		m.ID = m.CanonicalID()
	}
	if m.ID != m.CanonicalID() {
		return fmt.Errorf("MCP %q has an ID that does not match its definition", m.ID)
	}
	if err := os.MkdirAll(r.Paths.DataDir, 0o755); err != nil {
		return err
	}
	lock, err := acquireMCPRegistryLock(filepath.Join(r.Paths.DataDir, ".lock"))
	if err != nil {
		return err
	}
	defer releaseMCPRegistryLock(lock)
	if m.RegisteredAt.IsZero() {
		m.RegisteredAt = time.Now().UTC()
	}
	if existing, err := r.Get(m.ID); err == nil {
		// Re-registering the same canonical definition is idempotent and does
		// not refresh the timestamp or rewrite the file.
		if existing.CanonicalID() == m.CanonicalID() {
			return nil
		}
		return fmt.Errorf("MCP ID %q already contains a different definition", m.ID)
	}
	return AtomicWrite(r.Path(m.ID), []byte(formatMCP(m)), 0o644)
}

func (r MCPRegistry) Remove(id string) error {
	if err := os.MkdirAll(r.Paths.DataDir, 0o755); err != nil {
		return err
	}
	lock, err := acquireMCPRegistryLock(filepath.Join(r.Paths.DataDir, ".lock"))
	if err != nil {
		return err
	}
	defer releaseMCPRegistryLock(lock)
	if _, err := r.Get(id); err != nil {
		return err
	}
	return os.Remove(r.Path(id))
}

func acquireMCPRegistryLock(path string) (*os.File, error) {
	for i := 0; i < 100; i++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			return f, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil, fmt.Errorf("timed out waiting for MCP registry lock")
}

func releaseMCPRegistryLock(f *os.File) {
	path := f.Name()
	_ = f.Close()
	_ = os.Remove(path)
}

func ValidateMCP(m model.MCPMetadata) error {
	if err := model.ValidateMCPName(m.Name); err != nil {
		return err
	}
	switch m.Source.Type {
	case model.MCPSourceRegistry, model.MCPSourceRemote, model.MCPSourcePackage, model.MCPSourceCommand, model.MCPSourceImport:
	default:
		return fmt.Errorf("MCP %q uses unsupported source type %q", m.Name, m.Source.Type)
	}
	if m.Source.Type == model.MCPSourcePackage || m.Source.PackageType != "" || m.Source.PackageIdentifier != "" {
		switch m.Source.PackageType {
		case "npm", "pypi", "oci":
		default:
			return fmt.Errorf("MCP %q uses unsupported package type %q", m.Name, m.Source.PackageType)
		}
		if m.Source.PackageIdentifier == "" {
			return fmt.Errorf("MCP %q requires a package identifier", m.Name)
		}
	}
	if m.Source.Type == model.MCPSourceRegistry && m.Source.RegistryName == "" {
		return fmt.Errorf("MCP %q requires a registry name", m.Name)
	}
	if m.Source.Type == model.MCPSourceRegistry && m.Source.RegistryURL != "" {
		if err := validateHTTPURL(m.Name, m.Source.RegistryURL); err != nil {
			return fmt.Errorf("MCP registry URL: %w", err)
		}
	}
	if m.Source.Type == model.MCPSourceRemote && m.Transport != model.MCPTransportHTTP {
		return fmt.Errorf("MCP %q remote source requires HTTP transport", m.Name)
	}
	if (m.Source.Type == model.MCPSourcePackage || m.Source.Type == model.MCPSourceCommand) && m.Transport != model.MCPTransportStdio {
		return fmt.Errorf("MCP %q source %q requires stdio transport", m.Name, m.Source.Type)
	}
	if m.Transport != model.MCPTransportStdio && m.Transport != model.MCPTransportHTTP {
		return fmt.Errorf("MCP %q uses unsupported transport %q", m.Name, m.Transport)
	}
	if m.Transport == model.MCPTransportStdio {
		if m.Remote != nil {
			return fmt.Errorf("MCP %q cannot define a remote endpoint with stdio transport", m.Name)
		}
		if m.Command == nil || m.Command.Command == "" {
			return fmt.Errorf("MCP %q requires a command for stdio transport", m.Name)
		}
		if err := validateMCPString(m.Command.Command); err != nil {
			return err
		}
		for _, v := range m.Command.Args {
			if err := validateMCPString(v); err != nil {
				return err
			}
		}
		if err := validateMCPString(m.Command.Cwd); err != nil {
			return err
		}
		for k, v := range m.Command.Env {
			if !validEnvName(k) {
				return fmt.Errorf("invalid environment variable %q", k)
			}
			if err := validateMCPString(v); err != nil {
				return err
			}
		}
	} else {
		if m.Command != nil {
			return fmt.Errorf("MCP %q cannot define a command with HTTP transport", m.Name)
		}
		if m.Remote == nil || m.Remote.URL == "" {
			return fmt.Errorf("MCP %q requires a URL for HTTP transport", m.Name)
		}
		if err := validateMCPString(m.Remote.URL); err != nil {
			return err
		}
		if err := validateHTTPURL(m.Name, m.Remote.URL); err != nil {
			return err
		}
		for k, v := range m.Remote.Headers {
			if err := validateMCPString(k); err != nil {
				return err
			}
			if err := validateMCPString(v); err != nil {
				return err
			}
		}
		if m.Remote.Auth.Mode != "" && m.Remote.Auth.Mode != "none" && m.Remote.Auth.Mode != "oauth-auto" && m.Remote.Auth.Mode != "bearer-env" {
			return fmt.Errorf("MCP %q has unsupported auth mode %q", m.Name, m.Remote.Auth.Mode)
		}
		if m.Remote.Auth.Env != "" && !validEnvName(m.Remote.Auth.Env) {
			return fmt.Errorf("invalid auth environment variable %q", m.Remote.Auth.Env)
		}
		if m.Remote.Auth.Mode == "bearer-env" && m.Remote.Auth.Env == "" {
			return fmt.Errorf("MCP %q requires an auth environment variable", m.Name)
		}
	}
	seen := map[string]bool{}
	for _, req := range m.Requirements {
		if req.Name == "" {
			return fmt.Errorf("MCP %q has an invalid requirement", m.Name)
		}
		if req.Kind != "env" && req.Kind != "executable" {
			return fmt.Errorf("MCP %q uses unsupported requirement kind %q", m.Name, req.Kind)
		}
		if req.Kind == "env" && !validEnvName(req.Name) {
			return fmt.Errorf("invalid environment variable %q", req.Name)
		}
		key := req.Kind + ":" + req.Name
		if seen[key] {
			return fmt.Errorf("MCP requirement %q is duplicated", key)
		}
		seen[key] = true
	}
	return nil
}

func validateHTTPURL(name, value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("MCP %q requires an HTTP(S) URL", name)
	}
	return nil
}

func validateMCPString(v string) error { return model.ValidateMCPTemplate(v) }
func validEnvName(v string) bool {
	if v == "" {
		return false
	}
	for i, r := range v {
		if (i == 0 && !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_')) || (i > 0 && !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_')) {
			return false
		}
	}
	return true
}

func formatMCP(m model.MCPMetadata) string {
	var b strings.Builder
	version := m.SchemaVersion
	if version == 0 {
		version = 1
	}
	fmt.Fprintf(&b, "schema_version = %d\nid = %s\nname = %s\ndescription = %s\nregistered_at = %s\ntransport = %s\nreproducible = %t\n\n", version, quote(m.ID), quote(m.Name), quote(m.Description), quote(m.RegisteredAt.UTC().Format(time.RFC3339Nano)), quote(string(m.Transport)), m.Reproducible)
	fmt.Fprintf(&b, "[source]\ntype = %s\nregistry_url = %s\nregistry_name = %s\nversion = %s\nvariant = %s\npackage_type = %s\npackage_identifier = %s\npackage_version = %s\nimported_target = %s\n\n", quote(string(m.Source.Type)), quote(m.Source.RegistryURL), quote(m.Source.RegistryName), quote(m.Source.Version), quote(m.Source.Variant), quote(m.Source.PackageType), quote(m.Source.PackageIdentifier), quote(m.Source.PackageVersion), quote(m.Source.ImportedTarget))
	if m.Command != nil {
		env, _ := json.Marshal(m.Command.Env)
		fmt.Fprintf(&b, "[command]\ncommand = %s\nargs = %s\ncwd = %s\nenv_json = %s\n\n", quote(m.Command.Command), array(m.Command.Args), quote(m.Command.Cwd), quote(string(env)))
	}
	if m.Remote != nil {
		headers, _ := json.Marshal(m.Remote.Headers)
		fmt.Fprintf(&b, "[remote]\nurl = %s\nheaders_json = %s\nauth_mode = %s\nauth_env = %s\n\n", quote(m.Remote.URL), quote(string(headers)), quote(m.Remote.Auth.Mode), quote(m.Remote.Auth.Env))
	}
	for _, req := range m.Requirements {
		fmt.Fprintf(&b, "[[requirements]]\nkind = %s\nname = %s\nrequired = %t\nsecret = %t\ndescription = %s\n\n", quote(req.Kind), quote(req.Name), req.Required, req.Secret, quote(req.Description))
	}
	return b.String()
}

func parseMCPDocument(d document) (model.MCPMetadata, error) {
	get := func(section, key string, required bool) (string, error) {
		v, ok := d.scalars[section][key]
		if !ok {
			if required {
				return "", fmt.Errorf("MCP is missing %s.%s", section, key)
			}
			return "", nil
		}
		return parseString(v)
	}
	var m model.MCPMetadata
	var err error
	if m.ID, err = get("", "id", true); err != nil {
		return m, err
	}
	if m.Name, err = get("", "name", true); err != nil {
		return m, err
	}
	m.Description, _ = get("", "description", false)
	transport, _ := get("", "transport", true)
	m.Transport = model.MCPTransport(transport)
	m.RegisteredAt, _ = time.Parse(time.RFC3339Nano, mustGet(d, "", "registered_at"))
	m.Reproducible = boolValue(d.scalars[""]["reproducible"])
	m.Source.Type = model.MCPSourceType(mustGet(d, "source", "type"))
	m.Source.RegistryURL, _ = get("source", "registry_url", false)
	m.Source.RegistryName, _ = get("source", "registry_name", false)
	m.Source.Version, _ = get("source", "version", false)
	m.Source.Variant, _ = get("source", "variant", false)
	m.Source.PackageType, _ = get("source", "package_type", false)
	m.Source.PackageIdentifier, _ = get("source", "package_identifier", false)
	m.Source.PackageVersion, _ = get("source", "package_version", false)
	m.Source.ImportedTarget, _ = get("source", "imported_target", false)
	if _, ok := d.scalars["command"]; ok {
		c := &model.MCPCommand{}
		c.Command, _ = get("command", "command", true)
		c.Cwd, _ = get("command", "cwd", false)
		c.Args = d.arrays["command"]["args"]
		raw, _ := get("command", "env_json", false)
		if raw != "" {
			_ = json.Unmarshal([]byte(raw), &c.Env)
		}
		m.Command = c
	}
	if _, ok := d.scalars["remote"]; ok {
		x := &model.MCPRemote{}
		x.URL, _ = get("remote", "url", true)
		raw, _ := get("remote", "headers_json", false)
		if raw != "" {
			_ = json.Unmarshal([]byte(raw), &x.Headers)
		}
		x.Auth.Mode, _ = get("remote", "auth_mode", false)
		x.Auth.Env, _ = get("remote", "auth_env", false)
		m.Remote = x
	}
	for _, row := range d.arrayTables["requirements"] {
		req := model.MCPRequirement{}
		req.Kind, _ = parseString(row["kind"])
		req.Name, _ = parseString(row["name"])
		req.Required = boolValue(row["required"])
		req.Secret = boolValue(row["secret"])
		req.Description, _ = parseString(row["description"])
		m.Requirements = append(m.Requirements, req)
	}
	return m, ValidateMCP(m)
}
func mustGet(d document, section, key string) string {
	v, _ := d.scalars[section][key]
	s, _ := parseString(v)
	return s
}
