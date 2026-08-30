package mcpregistry

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ikts/cms/internal/model"
	"github.com/ikts/cms/internal/providers"
)

type Client struct {
	BaseURL    string
	CacheDir   string
	CacheTTL   time.Duration
	HTTPClient *http.Client
}

func New(base, cacheDir string, ttl time.Duration) *Client {
	if base == "" {
		base = "https://registry.modelcontextprotocol.io"
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &Client{BaseURL: strings.TrimRight(base, "/"), CacheDir: cacheDir, CacheTTL: ttl, HTTPClient: &http.Client{Timeout: 20 * time.Second}}
}
func (c *Client) Search(ctx context.Context, q string, limit int) ([]providers.MCPSearchResult, error) {
	var raw struct {
		Servers []json.RawMessage `json:"servers"`
	}
	if err := c.get(ctx, "/v0.1/servers?search="+url.QueryEscape(q), &raw); err != nil {
		return nil, err
	}
	out := make([]providers.MCPSearchResult, 0, len(raw.Servers))
	for _, b := range raw.Servers {
		var x registrySearchResponse
		if json.Unmarshal(b, &x) != nil {
			continue
		}
		server := x.server()
		out = append(out, server.toSearchResult(x.OfficialMetadata()))
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

type registrySearchResponse struct {
	registryServer
	Server registryServer            `json:"server"`
	Meta   map[string]map[string]any `json:"_meta"`
}

type registryServer struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Version     string `json:"version"`
	WebsiteURL  string `json:"websiteUrl"`
	Repository  struct {
		URL    string `json:"url"`
		Source string `json:"source"`
	} `json:"repository"`
	Packages []struct {
		RegistryType string `json:"registryType"`
		Identifier   string `json:"identifier"`
		Name         string `json:"name"`
		Version      string `json:"version"`
		RuntimeHint  string `json:"runtimeHint"`
	} `json:"packages"`
	Remotes []struct {
		Type        string `json:"type"`
		URL         string `json:"url"`
		URLTemplate string `json:"urlTemplate"`
	} `json:"remotes"`
	Icons []struct {
		Source   string   `json:"src"`
		MimeType string   `json:"mimeType"`
		Sizes    []string `json:"sizes"`
		Theme    string   `json:"theme"`
	} `json:"icons"`
}

type registryOfficialMetadata struct {
	Status        string
	StatusMessage string
	PublishedAt   string
	UpdatedAt     string
	IsLatest      bool
}

func (x registrySearchResponse) server() registryServer {
	server := x.Server
	if server.Name == "" {
		server = x.registryServer
	}
	return server
}

func (x registrySearchResponse) OfficialMetadata() registryOfficialMetadata {
	meta := registryOfficialMetadata{}
	for key, value := range x.Meta {
		if !strings.Contains(strings.ToLower(key), "official") {
			continue
		}
		meta.Status, _ = value["status"].(string)
		meta.StatusMessage, _ = value["statusMessage"].(string)
		meta.PublishedAt, _ = value["publishedAt"].(string)
		meta.UpdatedAt, _ = value["updatedAt"].(string)
		meta.IsLatest, _ = value["isLatest"].(bool)
	}
	return meta
}

func (s registryServer) toSearchResult(meta registryOfficialMetadata) providers.MCPSearchResult {
	result := providers.MCPSearchResult{Name: s.Name, Title: s.Title, Description: s.Description, Version: s.Version, WebsiteURL: s.WebsiteURL, RepositoryURL: s.Repository.URL, RepositorySource: s.Repository.Source, Status: meta.Status, StatusMessage: meta.StatusMessage, PublishedAt: meta.PublishedAt, UpdatedAt: meta.UpdatedAt, IsLatest: meta.IsLatest}
	for _, packageInfo := range s.Packages {
		identifier := packageInfo.Identifier
		if identifier == "" {
			identifier = packageInfo.Name
		}
		result.Packages = append(result.Packages, providers.MCPRegistryPackage{RegistryType: packageInfo.RegistryType, Identifier: identifier, Version: packageInfo.Version, RuntimeHint: packageInfo.RuntimeHint})
	}
	for _, remote := range s.Remotes {
		remoteURL := remote.URL
		if remoteURL == "" {
			remoteURL = remote.URLTemplate
		}
		result.Remotes = append(result.Remotes, providers.MCPRegistryRemote{Type: remote.Type, URL: remoteURL})
	}
	for _, icon := range s.Icons {
		result.Icons = append(result.Icons, providers.MCPRegistryIcon{Source: icon.Source, MimeType: icon.MimeType, Sizes: icon.Sizes, Theme: icon.Theme})
	}
	return result
}
func (c *Client) Resolve(ctx context.Context, ref providers.MCPProviderRef) ([]providers.MCPVariant, error) {
	path := "/v0.1/servers/" + url.PathEscape(ref.Name) + "/versions/latest"
	if ref.Version != "" {
		path = "/v0.1/servers/" + url.PathEscape(ref.Name) + "/versions/" + url.PathEscape(ref.Version)
	}
	var raw map[string]any
	if err := c.get(ctx, path, &raw); err != nil {
		return nil, err
	}
	if server, ok := raw["server"].(map[string]any); ok {
		raw = server
	}
	name, _ := raw["name"].(string)
	if name == "" {
		name = ref.Name
	}
	desc, _ := raw["description"].(string)
	version, _ := raw["version"].(string)
	if version == "" {
		version = ref.Version
	}
	var out []providers.MCPVariant
	if remotes, ok := raw["remotes"].([]any); ok {
		for _, v := range remotes {
			r, ok := v.(map[string]any)
			if !ok {
				continue
			}
			if transport := registryTransport(r, true); transport != "" && transport != "http" && transport != "streamable-http" {
				continue
			}
			u, _ := r["url"].(string)
			if u == "" {
				u, _ = r["urlTemplate"].(string)
			}
			if u != "" {
				out = append(out, providers.MCPVariant{Name: name, Description: desc, Version: version, Variant: "remote", Source: model.MCPSource{Type: model.MCPSourceRegistry, RegistryURL: c.BaseURL, RegistryName: ref.Name, Version: version, Variant: "remote"}, Transport: model.MCPTransportHTTP, Remote: &model.MCPRemote{URL: u}, Reproducible: version != ""})
			}
		}
	}
	if packages, ok := raw["packages"].([]any); ok {
		for _, v := range packages {
			p, ok := v.(map[string]any)
			if !ok {
				continue
			}
			if transport := registryTransport(p, false); transport != "" && transport != "stdio" {
				continue
			}
			kind, _ := p["registryType"].(string)
			if kind == "" {
				kind, _ = p["registry_type"].(string)
			}
			if kind != "npm" && kind != "pypi" && kind != "oci" {
				continue
			}
			identifier, _ := p["name"].(string)
			if identifier == "" {
				identifier, _ = p["identifier"].(string)
			}
			pv, _ := p["version"].(string)
			if pv == "" {
				pv = version
			}
			args := []string{}
			runtime := ""
			switch kind {
			case "npm":
				runtime = "npx"
				args = []string{"-y", identifier}
				if pv != "" {
					args = []string{"-y", identifier + "@" + pv}
				}
			case "pypi":
				runtime = "uvx"
				args = []string{identifier}
				if pv != "" {
					args = []string{identifier + "==" + pv}
				}
			case "oci":
				runtime = "docker"
				args = []string{"run", "--rm", identifier}
				if pv != "" {
					sep := ":"
					if strings.HasPrefix(pv, "sha256:") {
						sep = "@"
					}
					args = []string{"run", "--rm", identifier + sep + pv}
				}
			}
			if identifier != "" {
				reproducible := pv != "" && (kind != "oci" || strings.HasPrefix(pv, "sha256:"))
				out = append(out, providers.MCPVariant{Name: name, Description: desc, Version: version, Variant: kind, Source: model.MCPSource{Type: model.MCPSourceRegistry, RegistryURL: c.BaseURL, RegistryName: ref.Name, Version: version, Variant: kind, PackageType: kind, PackageIdentifier: identifier, PackageVersion: pv}, Transport: model.MCPTransportStdio, Command: &model.MCPCommand{Command: runtime, Args: args}, Reproducible: reproducible})
			}
		}
	}
	if ref.Variant != "" {
		var filtered []providers.MCPVariant
		for _, v := range out {
			if v.Variant == ref.Variant {
				filtered = append(filtered, v)
			}
		}
		out = filtered
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("MCP registry entry %q has no supported stdio or streamable-http variant", ref.Name)
	}
	return out, nil
}

func registryTransport(value map[string]any, includeType bool) string {
	keys := []string{"transportType", "transport_type"}
	if includeType {
		keys = append(keys, "type")
	}
	for _, key := range keys {
		if transport, _ := value[key].(string); transport != "" {
			return strings.ToLower(transport)
		}
	}
	if transport, ok := value["transport"].(map[string]any); ok {
		if kind, _ := transport["type"].(string); kind != "" {
			return strings.ToLower(kind)
		}
	}
	return ""
}
func (c *Client) get(ctx context.Context, path string, out any) error {
	var data []byte
	key := sha256.Sum256([]byte(c.BaseURL + path))
	cache := filepath.Join(c.CacheDir, fmt.Sprintf("%x.json", key[:]))
	if st, err := os.Stat(cache); err == nil && time.Since(st.ModTime()) < c.CacheTTL {
		data, err = os.ReadFile(cache)
		if err == nil {
			return json.Unmarshal(data, out)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("MCP Registry request failed: %w", err)
	}
	defer resp.Body.Close()
	data, err = io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("MCP Registry returned HTTP %d", resp.StatusCode)
	}
	_ = os.MkdirAll(c.CacheDir, 0o755)
	_ = os.WriteFile(cache, data, 0o644)
	return json.Unmarshal(data, out)
}

var _ providers.MCPProvider = (*Client)(nil)
