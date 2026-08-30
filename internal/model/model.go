package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

const SchemaVersion = 1

type MCPSourceType string

const (
	MCPSourceRegistry MCPSourceType = "registry"
	MCPSourceRemote   MCPSourceType = "remote"
	MCPSourcePackage  MCPSourceType = "package"
	MCPSourceCommand  MCPSourceType = "command"
	MCPSourceImport   MCPSourceType = "import"
)

type MCPTransport string

const (
	MCPTransportStdio MCPTransport = "stdio"
	MCPTransportHTTP  MCPTransport = "streamable-http"
)

type MCPSource struct {
	Type              MCPSourceType `json:"type"`
	RegistryURL       string        `json:"registry_url,omitempty"`
	RegistryName      string        `json:"registry_name,omitempty"`
	Version           string        `json:"version,omitempty"`
	Variant           string        `json:"variant,omitempty"`
	PackageType       string        `json:"package_type,omitempty"`
	PackageIdentifier string        `json:"package_identifier,omitempty"`
	PackageVersion    string        `json:"package_version,omitempty"`
	ImportedTarget    string        `json:"imported_target,omitempty"`
}

type MCPCommand struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Cwd     string            `json:"cwd,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

type MCPAuth struct {
	Mode string `json:"mode,omitempty"`
	Env  string `json:"env,omitempty"`
}
type MCPRemote struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Auth    MCPAuth           `json:"auth,omitempty"`
}
type MCPRequirement struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Required    bool   `json:"required"`
	Secret      bool   `json:"secret"`
	Description string `json:"description,omitempty"`
}

type MCPMetadata struct {
	SchemaVersion int              `json:"schema_version"`
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	Description   string           `json:"description,omitempty"`
	RegisteredAt  time.Time        `json:"registered_at"`
	Source        MCPSource        `json:"source"`
	Transport     MCPTransport     `json:"transport"`
	Command       *MCPCommand      `json:"command,omitempty"`
	Remote        *MCPRemote       `json:"remote,omitempty"`
	Requirements  []MCPRequirement `json:"requirements,omitempty"`
	Reproducible  bool             `json:"reproducible"`
}

type MCPToolFilter struct {
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
}
type MCPRef struct {
	ID    string        `json:"id"`
	Alias string        `json:"alias,omitempty"`
	Tools MCPToolFilter `json:"tools,omitempty"`
}
type ResolvedMCP struct {
	Metadata MCPMetadata `json:"metadata"`
	Ref      MCPRef      `json:"ref"`
}

var mcpSlugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

func ValidateMCPName(name string) error {
	if !mcpSlugRE.MatchString(name) {
		return fmt.Errorf("MCP name %q must use lowercase letters, numbers, _ or - and be at most 63 characters", name)
	}
	return nil
}

func ValidateMCPTemplate(s string) error {
	for i := 0; i < len(s); i++ {
		if s[i] == '\x00' || s[i] == '\n' || s[i] == '\r' {
			return fmt.Errorf("MCP values cannot contain control characters")
		}
		if s[i] != '$' || i+1 >= len(s) || s[i+1] != '{' {
			continue
		}
		end := strings.IndexByte(s[i+2:], '}')
		if end < 1 {
			return fmt.Errorf("invalid environment template")
		}
		name := s[i+2 : i+2+end]
		if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(name) {
			return fmt.Errorf("invalid environment variable %q", name)
		}
		i += end + 2
	}
	return nil
}

func (m MCPMetadata) CanonicalID() string {
	type canonical struct {
		Name         string
		Description  string
		Source       MCPSource
		Transport    MCPTransport
		Command      *MCPCommand
		Remote       *MCPRemote
		Requirements []MCPRequirement
	}
	r := append([]MCPRequirement(nil), m.Requirements...)
	sort.Slice(r, func(i, j int) bool {
		if r[i].Kind != r[j].Kind {
			return r[i].Kind < r[j].Kind
		}
		return r[i].Name < r[j].Name
	})
	b, _ := json.Marshal(canonical{m.Name, m.Description, m.Source, m.Transport, m.Command, m.Remote, r})
	h := sha256.Sum256(b)
	return m.Name + "@" + hex.EncodeToString(h[:])[:8]
}

func (m MCPMetadata) ExposedName(ref MCPRef) string {
	if ref.Alias != "" {
		return ref.Alias
	}
	return m.Name
}

// MCPDisplayLabels returns human-friendly labels for interactive listings.
// The canonical ID remains name@hash, but equal names need an origin suffix so
// users can distinguish different servers without decoding the hash.
func MCPDisplayLabels(mcps []MCPMetadata) map[string]string {
	counts := map[string]int{}
	for _, m := range mcps {
		counts[m.Name]++
	}
	labels := make(map[string]string, len(mcps))
	used := map[string]bool{}
	for _, m := range mcps {
		label := m.Name
		if label == "" {
			label = m.ID
		}
		if counts[m.Name] > 1 {
			if origin := m.DisplayOrigin(); origin != "" {
				label += " · " + origin
			} else {
				label += " · " + shortMCPID(m.ID)
			}
		}
		if used[label] {
			label += " · " + shortMCPID(m.ID)
		}
		used[label] = true
		labels[m.ID] = label
	}
	return labels
}

// DisplayOrigin returns the most useful stable origin identifier available
// for a registered MCP, without exposing credentials from remote URLs.
func (m MCPMetadata) DisplayOrigin() string {
	s := m.Source
	switch s.Type {
	case MCPSourceRegistry:
		return s.RegistryName
	case MCPSourcePackage:
		if s.PackageType != "" && s.PackageIdentifier != "" {
			return s.PackageType + ":" + s.PackageIdentifier
		}
		return s.PackageIdentifier
	case MCPSourceRemote:
		if m.Remote != nil {
			if u, err := url.Parse(m.Remote.URL); err == nil {
				return u.Host
			}
		}
	case MCPSourceCommand:
		if m.Command != nil {
			return m.Command.Command
		}
	case MCPSourceImport:
		return s.ImportedTarget
	}
	return string(s.Type)
}

func shortMCPID(id string) string {
	if i := strings.LastIndexByte(id, '@'); i >= 0 && i+1 < len(id) {
		return id[i+1:]
	}
	return id
}

func (f MCPToolFilter) Validate() error {
	for _, list := range [][]string{f.Allow, f.Deny} {
		seen := map[string]bool{}
		for _, name := range list {
			if name == "" {
				return fmt.Errorf("MCP tool names cannot be empty")
			}
			if seen[name] {
				return fmt.Errorf("MCP tool %q is duplicated", name)
			}
			seen[name] = true
		}
	}
	return nil
}

type SkillSource struct {
	Type       string
	Repository string
	Path       string
	Ref        string
	Commit     string
}

type SkillMetadata struct {
	SchemaVersion  int
	ID             string
	Name           string
	Description    string
	InstalledAt    time.Time
	InstallPath    string
	Source         SkillSource
	HasScripts     bool
	HasExecutables bool
}

type SkillRef struct{ ID string }

type Context struct {
	SchemaVersion int
	Name          string
	Description   string
	Skills        []SkillRef
	// MCPs is retained for schema-v1 source compatibility. MCPRefs is the v2
	// representation and carries aliases and tool filters.
	MCPs    []string
	MCPRefs []MCPRef
}

type ProjectLink struct {
	SkillID string
	Target  string
}

// ProjectManifest is the versioned, shareable dependency declaration for a
// project. Unlike ProjectState, it contains enough source information to
// reinstall missing skills on another machine.
type ProjectManifest struct {
	SchemaVersion int
	Context       string
	Description   string
	Targets       []string
	Skills        []PinnedSkill
	MCPs          []PinnedMCP
	ContextMCPs   []MCPRef
}

type PinnedSkill struct {
	ID          string
	Name        string
	Description string
	Source      SkillSource
}

type PinnedMCP struct{ Metadata MCPMetadata }

type ProjectState struct {
	SchemaVersion int
	Context       string
	Targets       []string
	Links         []ProjectLink
	MCPEntries    []MCPStateEntry
}

type MCPStateEntry struct {
	MCPID            string
	Target           string
	Scope            string
	Name             string
	ConfigPath       string
	Fingerprint      string
	ToolFilterStatus string
	ManagedKeys      []string
}

type Scope string

const (
	ScopeProject Scope = "project"
	ScopeGlobal  Scope = "global"
)
