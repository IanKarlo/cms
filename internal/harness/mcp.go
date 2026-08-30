package harness

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ikts/cms/internal/model"
)

type Capabilities struct{ Skills, MCP, MCPProject, MCPGlobal, MCPToolAllow, MCPToolDeny, MCPStdio, MCPHTTP bool }
type ToolFilterSupport struct{ Allow, Deny bool }
type MCPActionKind string

const (
	MCPCreate   MCPActionKind = "CREATE"
	MCPUpdate   MCPActionKind = "UPDATE"
	MCPRemove   MCPActionKind = "REMOVE"
	MCPKeep     MCPActionKind = "KEEP"
	MCPConflict MCPActionKind = "CONFLICT"
	MCPWarn     MCPActionKind = "WARNING"
)

type MCPAction struct {
	Kind        MCPActionKind `json:"kind"`
	Target      string        `json:"target"`
	ConfigPath  string        `json:"config_path,omitempty"`
	Name        string        `json:"name"`
	MCPID       string        `json:"mcp_id"`
	Reason      string        `json:"reason,omitempty"`
	ManagedKeys []string      `json:"managed_keys,omitempty"`
}
type MCPWarning struct {
	Code        string   `json:"code"`
	Target      string   `json:"target"`
	MCP         string   `json:"mcp"`
	Message     string   `json:"message"`
	Unsupported []string `json:"unsupported"`
}
type MCPPlan struct {
	Actions  []MCPAction
	Warnings []MCPWarning
	Data     []byte
	Entries  []model.MCPStateEntry
}

func (p MCPPlan) Conflicts() []MCPAction {
	var out []MCPAction
	for _, a := range p.Actions {
		if a.Kind == MCPConflict {
			out = append(out, a)
		}
	}
	return out
}

type MCPAdapter interface {
	Adapter
	ProjectMCPConfigPath(projectRoot string) string
	GlobalMCPConfigPath(home string) string
	ToolFilterSupport() ToolFilterSupport
	PlanMCPMutation(current []byte, desired []model.ResolvedMCP, managed []model.MCPStateEntry, scope model.Scope) (MCPPlan, error)
}
type GlobalSkillAdapter interface{ GlobalSkillDir(home string) string }

// Scope is kept in model so storage/state and adapters share one vocabulary.

func (Codex) Capabilities() Capabilities {
	return Capabilities{Skills: true, MCP: true, MCPProject: true, MCPGlobal: true, MCPToolAllow: true, MCPToolDeny: true, MCPStdio: true, MCPHTTP: true}
}
func (Codex) ProjectMCPConfigPath(root string) string {
	return filepath.Join(root, ".codex", "config.toml")
}
func (Codex) GlobalMCPConfigPath(home string) string {
	return filepath.Join(home, ".codex", "config.toml")
}
func (Codex) ToolFilterSupport() ToolFilterSupport { return ToolFilterSupport{Allow: true, Deny: true} }
func (Codex) GlobalSkillDir(home string) string    { return filepath.Join(home, ".agents", "skills") }
func (Codex) PlanMCPMutation(current []byte, desired []model.ResolvedMCP, managed []model.MCPStateEntry, scope model.Scope) (MCPPlan, error) {
	return planCodex(current, desired, managed, scope)
}

func (Claude) Capabilities() Capabilities {
	return Capabilities{Skills: true, MCP: true, MCPProject: true, MCPGlobal: true, MCPStdio: true, MCPHTTP: true}
}
func (Claude) ProjectMCPConfigPath(root string) string { return filepath.Join(root, ".mcp.json") }
func (Claude) GlobalMCPConfigPath(home string) string  { return filepath.Join(home, ".claude.json") }
func (Claude) ToolFilterSupport() ToolFilterSupport    { return ToolFilterSupport{} }
func (Claude) GlobalSkillDir(home string) string       { return filepath.Join(home, ".claude", "skills") }
func (Claude) PlanMCPMutation(current []byte, desired []model.ResolvedMCP, managed []model.MCPStateEntry, scope model.Scope) (MCPPlan, error) {
	return planJSONMCP(current, desired, managed, scope, "claude", jsonMCPConfig{key: "mcpServers", style: "claude"})
}

type Antigravity struct{}

func (Antigravity) ID() string                       { return "antigravity" }
func (Antigravity) Detect(root string) (bool, error) { return true, nil }
func (Antigravity) SkillDir(root string) string      { return filepath.Join(root, ".agents", "skills") }
func (Antigravity) SupportsSymlink() bool            { return true }
func (Antigravity) Capabilities() Capabilities {
	return Capabilities{Skills: true, MCP: true, MCPProject: true, MCPGlobal: true, MCPToolDeny: true, MCPStdio: true, MCPHTTP: true}
}
func (Antigravity) ProjectMCPConfigPath(root string) string {
	return filepath.Join(root, ".agents", "mcp_config.json")
}
func (Antigravity) GlobalMCPConfigPath(home string) string {
	return filepath.Join(home, ".gemini", "config", "mcp_config.json")
}
func (Antigravity) ToolFilterSupport() ToolFilterSupport { return ToolFilterSupport{Deny: true} }
func (Antigravity) GlobalSkillDir(home string) string {
	return filepath.Join(home, ".gemini", "config", "skills")
}
func (Antigravity) PlanMCPMutation(current []byte, desired []model.ResolvedMCP, managed []model.MCPStateEntry, scope model.Scope) (MCPPlan, error) {
	return planJSONMCP(current, desired, managed, scope, "antigravity", jsonMCPConfig{key: "mcpServers", style: "antigravity"})
}

type Cursor struct{}

func (Cursor) ID() string                       { return "cursor" }
func (Cursor) Detect(root string) (bool, error) { return true, nil }
func (Cursor) SkillDir(root string) string      { return filepath.Join(root, ".cursor", "skills") }
func (Cursor) SupportsSymlink() bool            { return true }
func (Cursor) Capabilities() Capabilities {
	return Capabilities{Skills: true, MCP: true, MCPProject: true, MCPGlobal: true, MCPStdio: true, MCPHTTP: true}
}
func (Cursor) ProjectMCPConfigPath(root string) string {
	return filepath.Join(root, ".cursor", "mcp.json")
}
func (Cursor) GlobalMCPConfigPath(home string) string {
	return filepath.Join(home, ".cursor", "mcp.json")
}
func (Cursor) ToolFilterSupport() ToolFilterSupport { return ToolFilterSupport{} }
func (Cursor) GlobalSkillDir(home string) string    { return filepath.Join(home, ".cursor", "skills") }
func (Cursor) PlanMCPMutation(c []byte, d []model.ResolvedMCP, m []model.MCPStateEntry, s model.Scope) (MCPPlan, error) {
	return planJSONMCP(c, d, m, s, "cursor", jsonMCPConfig{key: "mcpServers", style: "cursor"})
}

type OpenCode struct{}

func (OpenCode) ID() string                       { return "opencode" }
func (OpenCode) Detect(root string) (bool, error) { return true, nil }
func (OpenCode) SkillDir(root string) string      { return filepath.Join(root, ".opencode", "skills") }
func (OpenCode) SupportsSymlink() bool            { return true }
func (OpenCode) Capabilities() Capabilities {
	return Capabilities{Skills: true, MCP: true, MCPProject: true, MCPGlobal: true, MCPToolDeny: true, MCPStdio: true, MCPHTTP: true}
}
func (OpenCode) ProjectMCPConfigPath(root string) string { return filepath.Join(root, "opencode.json") }
func (OpenCode) GlobalMCPConfigPath(home string) string {
	return filepath.Join(home, ".config", "opencode", "opencode.json")
}
func (OpenCode) ToolFilterSupport() ToolFilterSupport { return ToolFilterSupport{Deny: true} }
func (OpenCode) GlobalSkillDir(home string) string {
	return filepath.Join(home, ".config", "opencode", "skills")
}
func (OpenCode) PlanMCPMutation(c []byte, d []model.ResolvedMCP, m []model.MCPStateEntry, s model.Scope) (MCPPlan, error) {
	return planJSONMCP(c, d, m, s, "opencode", jsonMCPConfig{key: "mcp", style: "opencode"})
}

type jsonMCPConfig struct{ key, style string }

func planJSONMCP(current []byte, desired []model.ResolvedMCP, managed []model.MCPStateEntry, scope model.Scope, target string, cfg jsonMCPConfig) (MCPPlan, error) {
	var root map[string]any
	if len(bytes.TrimSpace(current)) == 0 {
		root = map[string]any{}
	} else if err := json.Unmarshal(stripJSONComments(current), &root); err != nil {
		return MCPPlan{}, fmt.Errorf("parse %s MCP config: %w", target, err)
	}
	servers := map[string]any{}
	if v, ok := root[cfg.key].(map[string]any); ok {
		servers = v
	} else if root[cfg.key] != nil {
		return MCPPlan{}, fmt.Errorf("%s MCP config field %q is not an object", target, cfg.key)
	}
	managedByName := map[string]model.MCPStateEntry{}
	for _, e := range managed {
		managedByName[e.Name] = e
	}
	topTools := map[string]any{}
	if cfg.style == "opencode" {
		if v, ok := root["tools"].(map[string]any); ok {
			topTools = v
		} else if root["tools"] != nil {
			return MCPPlan{}, fmt.Errorf("opencode tools field is not an object")
		}
	}
	desiredNames := map[string]bool{}
	p := MCPPlan{Data: current}
	for _, r := range desired {
		if target == "antigravity" {
			if err := validateAntigravity(r); err != nil {
				return MCPPlan{}, err
			}
		}
		name := r.Metadata.ExposedName(r.Ref)
		desiredNames[name] = true
		value := renderJSONMCP(r, target, cfg.style)
		keys := []string{cfg.key + "." + name}
		old, exists := servers[name]
		entry, known := managedByName[name]
		toolConflict := false
		if cfg.style == "opencode" && known {
			for _, k := range entry.ManagedKeys {
				if strings.HasPrefix(k, "tools.") {
					toolKey := strings.TrimPrefix(k, "tools.")
					if value, ok := topTools[toolKey]; !ok || value != false {
						p.Actions = append(p.Actions, MCPAction{Kind: MCPConflict, Target: target, Name: name, MCPID: r.Metadata.ID, Reason: "CMS-managed OpenCode tool rule was modified externally"})
						toolConflict = true
						break
					}
				}
			}
		}
		if toolConflict {
			continue
		}
		if exists && !known {
			p.Actions = append(p.Actions, MCPAction{Kind: MCPConflict, Target: target, Name: name, MCPID: r.Metadata.ID, Reason: "an MCP entry with this name is not managed by CMS"})
			continue
		}
		if known && fingerprintJSON(old) != managedByName[name].Fingerprint {
			p.Actions = append(p.Actions, MCPAction{Kind: MCPConflict, Target: target, Name: name, MCPID: r.Metadata.ID, Reason: "CMS-managed MCP entry was modified externally"})
			continue
		}
		if cfg.style == "opencode" {
			managedTools := map[string]bool{}
			if known {
				for _, k := range entry.ManagedKeys {
					if strings.HasPrefix(k, "tools.") {
						managedTools[strings.TrimPrefix(k, "tools.")] = true
					}
				}
			}
			wantedTools := map[string]bool{}
			for _, tool := range r.Ref.Tools.Deny {
				toolKey := name + "_" + tool
				wantedTools[toolKey] = true
				if _, present := topTools[toolKey]; present && !managedTools[toolKey] {
					p.Actions = append(p.Actions, MCPAction{Kind: MCPConflict, Target: target, Name: name, MCPID: r.Metadata.ID, Reason: fmt.Sprintf("OpenCode tool rule %q is not managed by CMS", toolKey)})
					toolConflict = true
					break
				}
				topTools[toolKey] = false
				keys = append(keys, "tools."+toolKey)
			}
			if toolConflict {
				continue
			}
			for toolKey := range managedTools {
				if !wantedTools[toolKey] {
					delete(topTools, toolKey)
				}
			}
		}
		kind := MCPCreate
		if exists {
			kind = MCPUpdate
		}
		if known && exists && managedByName[name].MCPID == r.Metadata.ID && fingerprintJSON(value) == entry.Fingerprint && sameStringSet(keys, entry.ManagedKeys) {
			kind = MCPKeep
		}
		p.Actions = append(p.Actions, MCPAction{Kind: kind, Target: target, Name: name, MCPID: r.Metadata.ID, ManagedKeys: keys})
		servers[name] = value
		p.Entries = append(p.Entries, stateEntry(r, target, scope, name, fingerprintJSON(value), filterStatus(r.Ref.Tools, target), keys))
	}
	for name, e := range managedByName {
		if desiredNames[name] {
			continue
		}
		if value, ok := servers[name]; ok && fingerprintJSON(value) != e.Fingerprint {
			p.Actions = append(p.Actions, MCPAction{Kind: MCPConflict, Target: target, Name: name, MCPID: e.MCPID, Reason: "CMS-managed MCP entry was modified externally"})
			continue
		}
		delete(servers, name)
		if cfg.style == "opencode" {
			for _, k := range e.ManagedKeys {
				if strings.HasPrefix(k, "tools.") {
					delete(topTools, strings.TrimPrefix(k, "tools."))
				}
			}
		}
		keys := e.ManagedKeys
		if len(keys) == 0 {
			keys = []string{cfg.key + "." + name}
		}
		p.Actions = append(p.Actions, MCPAction{Kind: MCPRemove, Target: target, Name: name, MCPID: e.MCPID, ManagedKeys: keys})
	}
	root[cfg.key] = servers
	if cfg.style == "opencode" {
		root["tools"] = topTools
	}
	if len(bytes.TrimSpace(current)) == 0 {
		out, err := json.MarshalIndent(root, "", "  ")
		if err != nil {
			return MCPPlan{}, err
		}
		p.Data = append(out, '\n')
	} else {
		out, err := patchJSONCMCP(current, cfg.key, servers, topTools, p.Actions, managed, p.Entries)
		if err != nil {
			return MCPPlan{}, fmt.Errorf("patch %s MCP config: %w", target, err)
		}
		p.Data = out
	}
	for _, r := range desired {
		if w := filterWarning(r, target); w.Code != "" {
			p.Warnings = append(p.Warnings, w)
		}
	}
	return p, nil
}

func renderJSONMCP(r model.ResolvedMCP, target, style string) map[string]any {
	m := r.Metadata
	out := map[string]any{}
	if target == "opencode" {
		if m.Transport == model.MCPTransportStdio {
			out["type"] = "local"
			out["command"] = append([]string{m.Command.Command}, m.Command.Args...)
			if len(m.Command.Env) > 0 {
				env := map[string]string{}
				for k, v := range m.Command.Env {
					env[k] = convertTemplate(v, target)
				}
				out["environment"] = env
			}
		} else {
			out["type"] = "remote"
			out["url"] = m.Remote.URL
		}
		out["enabled"] = true
		if m.Remote != nil && (len(m.Remote.Headers) > 0 || m.Remote.Auth.Mode == "bearer-env") {
			h := map[string]string{}
			for k, v := range m.Remote.Headers {
				h[k] = convertTemplate(v, target)
			}
			if m.Remote.Auth.Mode == "bearer-env" {
				h["Authorization"] = convertTemplate("Bearer ${"+m.Remote.Auth.Env+"}", target)
			}
			out["headers"] = h
		}
		return out
	}
	if m.Transport == model.MCPTransportStdio {
		out["type"] = "stdio"
		out["command"] = m.Command.Command
		out["args"] = m.Command.Args
		if len(m.Command.Env) > 0 {
			env := map[string]string{}
			for k, v := range m.Command.Env {
				env[k] = convertTemplate(v, target)
			}
			out["env"] = env
		}
	} else {
		if style == "antigravity" {
			out["serverUrl"] = m.Remote.URL
		} else {
			out["type"] = "http"
			out["url"] = m.Remote.URL
		}
		if len(m.Remote.Headers) > 0 || m.Remote.Auth.Mode == "bearer-env" {
			h := map[string]string{}
			for k, v := range m.Remote.Headers {
				h[k] = convertTemplate(v, target)
			}
			if m.Remote.Auth.Mode == "bearer-env" {
				h["Authorization"] = convertTemplate("Bearer ${"+m.Remote.Auth.Env+"}", target)
			}
			out["headers"] = h
		}
	}
	if style == "antigravity" && len(r.Ref.Tools.Deny) > 0 {
		out["disabledTools"] = r.Ref.Tools.Deny
	}
	return out
}
func convertTemplate(s, target string) string {
	if target == "cursor" {
		return convertEnv(s, "${", "}", "${env:", "}")
	}
	if target == "opencode" {
		return convertEnv(s, "${", "}", "{env:", "}")
	}
	return s
}

func validateAntigravity(r model.ResolvedMCP) error {
	if r.Metadata.Remote != nil {
		for k, v := range r.Metadata.Remote.Headers {
			if len(mcpEnvRefs(v)) > 0 {
				return fmt.Errorf("target antigravity cannot safely materialize header %q for MCP %q without persisting secret %s", k, r.Metadata.ExposedName(r.Ref), mcpEnvRefs(v)[0])
			}
		}
	}
	if r.Metadata.Remote != nil && r.Metadata.Remote.Auth.Mode == "bearer-env" {
		return fmt.Errorf("target antigravity cannot safely materialize header %q for MCP %q without persisting secret %s", "Authorization", r.Metadata.ExposedName(r.Ref), r.Metadata.Remote.Auth.Env)
	}
	if r.Metadata.Command != nil {
		for k, v := range r.Metadata.Command.Env {
			if len(mcpEnvRefs(v)) > 0 {
				return fmt.Errorf("target antigravity cannot safely materialize env %q for MCP %q without a supported env binding", k, r.Metadata.ExposedName(r.Ref))
			}
		}
	}
	return nil
}
func mcpEnvRefs(v string) []string {
	var out []string
	for {
		start := strings.Index(v, "${")
		if start < 0 {
			break
		}
		end := strings.Index(v[start+2:], "}")
		if end < 0 {
			break
		}
		out = append(out, v[start+2:start+2+end])
		v = v[start+3+end:]
	}
	return out
}
func convertEnv(s, open, close, replOpen, replClose string) string {
	var b strings.Builder
	for len(s) > 0 {
		start := strings.Index(s, open)
		if start < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:start])
		end := strings.Index(s[start+len(open):], close)
		if end < 0 {
			b.WriteString(s[start:])
			break
		}
		end += start + len(open)
		b.WriteString(replOpen)
		b.WriteString(s[start+len(open) : end])
		b.WriteString(replClose)
		s = s[end+len(close):]
	}
	return b.String()
}
func stripJSONComments(b []byte) []byte {
	var out strings.Builder
	inString, escaped := false, false
	for i := 0; i < len(b); {
		c := b[i]
		if inString {
			out.WriteByte(c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			i++
			continue
		}
		if c == '"' {
			inString = true
			out.WriteByte(c)
			i++
			continue
		}
		if c == '/' && i+1 < len(b) && b[i+1] == '/' {
			i += 2
			for i < len(b) && b[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(b) && b[i+1] == '*' {
			i += 2
			for i+1 < len(b) && !(b[i] == '*' && b[i+1] == '/') {
				if b[i] == '\n' {
					out.WriteByte('\n')
				}
				i++
			}
			if i+1 < len(b) {
				i += 2
			}
			continue
		}
		out.WriteByte(c)
		i++
	}
	return stripTrailingJSONCommas([]byte(out.String()))
}
func stripTrailingJSONCommas(data []byte) []byte {
	var out strings.Builder
	inString, escaped := false, false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if inString {
			out.WriteByte(c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			out.WriteByte(c)
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(data) && (data[j] == ' ' || data[j] == '\n' || data[j] == '\r' || data[j] == '\t') {
				j++
			}
			if j < len(data) && (data[j] == '}' || data[j] == ']') {
				continue
			}
		}
		out.WriteByte(c)
	}
	return []byte(out.String())
}
func preserveJSONComments(original, rendered []byte) []byte {
	comments := scanJSONComments(string(original))
	if len(comments) == 0 {
		return rendered
	}
	lines := strings.Split(string(rendered), "\n")
	for _, comment := range comments {
		anchor := comment.anchor
		line := -1
		if anchor != "" {
			for i, candidate := range lines {
				if strings.Contains(candidate, anchor+":") {
					line = i
					break
				}
			}
		}
		if line < 0 {
			line = 0
		}
		indent := comment.indent
		if indent == "" && line < len(lines) {
			indent = leadingWhitespace(lines[line])
		}
		text := indent + comment.text
		if comment.before {
			lines = insertLines(lines, line, strings.Split(text, "\n"))
		} else {
			at := line + 1
			for at < len(lines) && isJSONCommentLine(lines[at]) {
				at++
			}
			lines = insertLines(lines, at, strings.Split(text, "\n"))
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

type jsonComment struct {
	text, anchor, indent string
	before               bool
}

func scanJSONComments(source string) []jsonComment {
	var comments []jsonComment
	inString, escaped := false, false
	lastKey := ""
	for i := 0; i < len(source); {
		if source[i] == '"' && !inString {
			if key, end, ok := jsonKeyAt(source, i); ok {
				lastKey = key
				i = end
				continue
			}
			inString = true
			i++
			continue
		}
		if inString {
			if escaped {
				escaped = false
			} else if source[i] == '\\' {
				escaped = true
			} else if source[i] == '"' {
				inString = false
			}
			i++
			continue
		}
		if source[i] != '/' || i+1 >= len(source) || (source[i+1] != '/' && source[i+1] != '*') {
			i++
			continue
		}
		lineStart := strings.LastIndexByte(source[:i], '\n') + 1
		indent := leadingWhitespace(source[lineStart:i])
		before := strings.TrimSpace(source[lineStart:i]) == ""
		anchor := lastKey
		end := i + 2
		if source[i+1] == '/' {
			if next := strings.IndexByte(source[end:], '\n'); next >= 0 {
				end += next
			} else {
				end = len(source)
			}
		} else if close := strings.Index(source[end:], "*/"); close >= 0 {
			end += close + 2
		} else {
			end = len(source)
		}
		if before {
			if nextKey := nextJSONKey(source, end); nextKey != "" {
				anchor = nextKey
				before = true
			}
		} else {
			// A trailing comment belongs to the outer property on its line;
			// using the last nested key would move it into a different object
			// after JSON is re-rendered.
			if keys := jsonKeysInText(source[lineStart:i]); len(keys) > 0 {
				anchor = keys[0]
			} else if anchor == "" {
				anchor = previousJSONKey(source[:i])
			}
		}
		comments = append(comments, jsonComment{text: source[i:end], anchor: anchor, indent: indent, before: before})
		i = end
	}
	return comments
}

func jsonKeyAt(source string, start int) (string, int, bool) {
	end := start + 1
	escaped := false
	for end < len(source) {
		if escaped {
			escaped = false
		} else if source[end] == '\\' {
			escaped = true
		} else if source[end] == '"' {
			break
		}
		end++
	}
	if end >= len(source) {
		return "", end, false
	}
	j := end + 1
	for j < len(source) && (source[j] == ' ' || source[j] == '\t' || source[j] == '\r' || source[j] == '\n') {
		j++
	}
	if j >= len(source) || source[j] != ':' {
		return "", end + 1, false
	}
	return source[start : end+1], end + 1, true
}

func nextJSONKey(source string, start int) string {
	inString, escaped := false, false
	for i := start; i < len(source); i++ {
		if inString {
			if escaped {
				escaped = false
			} else if source[i] == '\\' {
				escaped = true
			} else if source[i] == '"' {
				inString = false
			}
			continue
		}
		if source[i] == '"' {
			if key, _, ok := jsonKeyAt(source, i); ok {
				return key
			}
			inString = true
		}
	}
	return ""
}

func previousJSONKey(source string) string {
	for i := len(source) - 1; i >= 0; i-- {
		if source[i] == '"' {
			if key, _, ok := jsonKeyAt(source, i); ok {
				return key
			}
		}
	}
	return ""
}

func jsonKeysInText(source string) []string {
	var keys []string
	for i := 0; i < len(source); i++ {
		if source[i] != '"' {
			continue
		}
		if key, end, ok := jsonKeyAt(source, i); ok {
			keys = append(keys, key)
			i = end - 1
		}
	}
	return keys
}

func leadingWhitespace(s string) string {
	return s[:len(s)-len(strings.TrimLeft(s, " \t"))]
}

func insertLines(lines []string, at int, additions []string) []string {
	if at < 0 {
		at = 0
	}
	if at > len(lines) {
		at = len(lines)
	}
	out := make([]string, 0, len(lines)+len(additions))
	out = append(out, lines[:at]...)
	out = append(out, additions...)
	out = append(out, lines[at:]...)
	return out
}

func isJSONCommentLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") || strings.HasSuffix(trimmed, "*/")
}
func fingerprintJSON(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func stateEntry(r model.ResolvedMCP, target string, scope model.Scope, name, fp, status string, keys []string) model.MCPStateEntry {
	return model.MCPStateEntry{MCPID: r.Metadata.ID, Target: target, Scope: string(scope), Name: name, Fingerprint: fp, ToolFilterStatus: status, ManagedKeys: keys}
}
func filterStatus(f model.MCPToolFilter, target string) string {
	support := map[string]ToolFilterSupport{"codex": {true, true}, "antigravity": {false, true}, "opencode": {false, true}}[target]
	if len(f.Allow) > 0 && !support.Allow {
		return "partial"
	}
	if len(f.Deny) > 0 && !support.Deny {
		return "unsupported"
	}
	return "complete"
}
func filterWarning(r model.ResolvedMCP, target string) MCPWarning {
	support := map[string]ToolFilterSupport{"codex": {true, true}, "antigravity": {false, true}, "opencode": {false, true}}[target]
	var u []string
	if len(r.Ref.Tools.Allow) > 0 && !support.Allow {
		u = append(u, "allow")
	}
	if len(r.Ref.Tools.Deny) > 0 && !support.Deny {
		u = append(u, "deny")
	}
	if len(u) == 0 {
		return MCPWarning{}
	}
	code := "mcp_tool_filter_partial"
	if len(u) == 2 {
		code = "mcp_tool_filter_unsupported"
	}
	return MCPWarning{Code: code, Target: target, MCP: r.Metadata.ExposedName(r.Ref), Unsupported: u, Message: fmt.Sprintf("target %q cannot fully enforce the requested tool filter for MCP %q", target, r.Metadata.ExposedName(r.Ref))}
}

func planCodex(current []byte, desired []model.ResolvedMCP, managed []model.MCPStateEntry, scope model.Scope) (MCPPlan, error) {
	text := string(current)
	sections := splitTomlSections(text)
	byName := map[string]tomlSection{}
	for _, s := range sections {
		if name, ok := codexServerName(s.name); ok {
			byName[name] = s
		}
	}
	managedByName := map[string]model.MCPStateEntry{}
	for _, e := range managed {
		managedByName[e.Name] = e
	}
	wanted := map[string]bool{}
	p := MCPPlan{}
	for _, r := range desired {
		name := r.Metadata.ExposedName(r.Ref)
		wanted[name] = true
		old, exists := byName[name]
		e, known := managedByName[name]
		if exists && !known {
			p.Actions = append(p.Actions, MCPAction{Kind: MCPConflict, Target: "codex", Name: name, MCPID: r.Metadata.ID, Reason: "an MCP entry with this name is not managed by CMS"})
			continue
		}
		if known && fingerprintBytes([]byte(old.body)) != e.Fingerprint {
			p.Actions = append(p.Actions, MCPAction{Kind: MCPConflict, Target: "codex", Name: name, MCPID: r.Metadata.ID, Reason: "CMS-managed MCP entry was modified externally"})
			continue
		}
		body := renderCodex(r)
		kind := MCPCreate
		if exists {
			kind = MCPUpdate
		}
		if known && exists && e.MCPID == r.Metadata.ID && fingerprintBytes([]byte(body)) == e.Fingerprint {
			kind = MCPKeep
		}
		p.Actions = append(p.Actions, MCPAction{Kind: kind, Target: "codex", Name: name, MCPID: r.Metadata.ID, ManagedKeys: []string{"mcp_servers." + name}})
		fingerprint := fingerprintBytes([]byte(body))
		if kind == MCPKeep {
			body = old.body
			fingerprint = e.Fingerprint
		}
		byName[name] = tomlSection{name: "mcp_servers." + name, body: body}
		p.Entries = append(p.Entries, stateEntry(r, "codex", scope, name, fingerprint, filterStatus(r.Ref.Tools, "codex"), []string{"mcp_servers." + name}))
	}
	for name, e := range managedByName {
		if wanted[name] {
			continue
		}
		old, ok := byName[name]
		if !ok {
			continue
		}
		if fingerprintBytes([]byte(old.body)) != e.Fingerprint {
			p.Actions = append(p.Actions, MCPAction{Kind: MCPConflict, Target: "codex", Name: name, MCPID: e.MCPID, Reason: "CMS-managed MCP entry was modified externally"})
			continue
		}
		delete(byName, name)
		p.Actions = append(p.Actions, MCPAction{Kind: MCPRemove, Target: "codex", Name: name, MCPID: e.MCPID, ManagedKeys: []string{"mcp_servers." + name}})
	}
	// Replace only CMS sections and append new sections; all other TOML stays byte-for-byte.
	p.Data = []byte(rebuildToml(text, byName))
	for _, r := range desired {
		if w := filterWarning(r, "codex"); w.Code != "" {
			p.Warnings = append(p.Warnings, w)
		}
	}
	return p, nil
}

type tomlSection struct{ name, body string }

func splitTomlSections(s string) []tomlSection {
	lines := strings.SplitAfter(s, "\n")
	var out []tomlSection
	name := ""
	var body strings.Builder
	flush := func() {
		if body.Len() > 0 {
			out = append(out, tomlSection{name, body.String()})
		}
	}
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "[") && strings.HasSuffix(trim, "]") && !strings.HasPrefix(trim, "[[") {
			flush()
			name = strings.TrimSpace(trim[1 : len(trim)-1])
			body.Reset()
			body.WriteString(line)
		} else {
			body.WriteString(line)
		}
	}
	flush()
	return out
}
func rebuildToml(original string, sections map[string]tomlSection) string {
	parts := splitTomlSections(original)
	var b strings.Builder
	seen := map[string]bool{}
	for _, part := range parts {
		if name, isMCP := codexServerName(part.name); isMCP {
			if replacement, ok := sections[name]; ok {
				b.WriteString(replacement.body)
				seen[name] = true
			}
			continue
		}
		b.WriteString(part.body)
	}
	names := make([]string, 0, len(sections))
	for n := range sections {
		if !seen[n] {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n\n") {
			b.WriteString("\n")
		}
		b.WriteString(sections[n].body)
	}
	return b.String()
}

func codexServerName(section string) (string, bool) {
	if !strings.HasPrefix(section, "mcp_servers.") {
		return "", false
	}
	name := strings.TrimPrefix(section, "mcp_servers.")
	if strings.HasPrefix(name, "\"") {
		unquoted, err := strconv.Unquote(name)
		if err != nil {
			return name, true
		}
		name = unquoted
	}
	return name, true
}
func renderCodex(r model.ResolvedMCP) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[mcp_servers.%s]\n", r.Metadata.ExposedName(r.Ref))
	m := r.Metadata
	if m.Transport == model.MCPTransportStdio {
		fmt.Fprintf(&b, "command = %q\nargs = %s\n", m.Command.Command, tomlArray(append([]string(nil), m.Command.Args...)))
		if len(m.Command.Env) > 0 {
			fmt.Fprintf(&b, "env = %s\n", tomlStringMap(m.Command.Env))
		}
	} else {
		fmt.Fprintf(&b, "url = %q\n", m.Remote.URL)
		if m.Remote.Auth.Mode == "bearer-env" {
			fmt.Fprintf(&b, "bearer_token_env_var = %q\n", m.Remote.Auth.Env)
		}
		if len(m.Remote.Headers) > 0 {
			fmt.Fprintf(&b, "http_headers = %s\n", tomlStringMap(m.Remote.Headers))
		}
	}
	if len(r.Ref.Tools.Allow) > 0 {
		fmt.Fprintf(&b, "enabled_tools = %s\n", tomlArray(r.Ref.Tools.Allow))
	}
	if len(r.Ref.Tools.Deny) > 0 {
		fmt.Fprintf(&b, "disabled_tools = %s\n", tomlArray(r.Ref.Tools.Deny))
	}
	return b.String()
}
func tomlArray(v []string) string {
	q := make([]string, len(v))
	for i, s := range v {
		q[i] = fmt.Sprintf("%q", s)
	}
	return "[" + strings.Join(q, ", ") + "]"
}
func tomlStringMap(v map[string]string) string {
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var p []string
	for _, k := range keys {
		p = append(p, fmt.Sprintf("%q = %q", k, v[k]))
	}
	return "{" + strings.Join(p, ", ") + "}"
}
func fingerprintBytes(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, value := range a {
		seen[value]++
	}
	for _, value := range b {
		seen[value]--
		if seen[value] < 0 {
			return false
		}
	}
	return true
}

// Atomic config replacement must preserve permissions of an existing file.
func WriteMCPConfig(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	perm := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		perm = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".cms-mcp-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(perm); err == nil {
		_, err = tmp.Write(data)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}
