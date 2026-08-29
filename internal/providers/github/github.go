package github

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ikts/cms/internal/model"
	"github.com/ikts/cms/internal/providers"
	"github.com/ikts/cms/internal/skills"
	"github.com/ikts/cms/internal/storage"
)

type Candidate = providers.SearchResult

type APIError struct {
	Code    int
	Status  string
	Message string
}

func (e *APIError) Error() string { return e.Message }

var _ providers.SkillProvider = Provider{}
var _ providers.BatchSkillProvider = Provider{}

type Provider struct {
	Client       *http.Client
	Token        string
	APIBase      string
	RawBase      string
	DownloadBase string
	CacheDir     string
	CacheTTL     time.Duration
}

func New() Provider {
	return Provider{Client: &http.Client{Timeout: 30 * time.Second}, Token: os.Getenv("GITHUB_TOKEN"), APIBase: "https://api.github.com", RawBase: "https://raw.githubusercontent.com", DownloadBase: "https://codeload.github.com"}
}

func NewWithCache(cacheDir string, ttl time.Duration) Provider {
	p := New()
	p.CacheDir, p.CacheTTL = cacheDir, ttl
	return p
}

func (p Provider) request(ctx context.Context, method, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "cms/0.1")
	if p.Token != "" {
		req.Header.Set("Authorization", "Bearer "+p.Token)
	}
	resp, err := p.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == 401 {
			return nil, &APIError{Code: resp.StatusCode, Status: resp.Status, Message: "GitHub Code Search requires authentication. Set GITHUB_TOKEN with a GitHub token and retry"}
		}
		if resp.StatusCode == 403 || resp.StatusCode == 429 {
			return nil, &APIError{Code: resp.StatusCode, Status: resp.Status, Message: "GitHub search rate limit exceeded; set GITHUB_TOKEN and retry, or wait until the API limit resets"}
		}
		return nil, &APIError{Code: resp.StatusCode, Status: resp.Status, Message: fmt.Sprintf("GitHub API returned %s: %s", resp.Status, strings.TrimSpace(string(body)))}
	}
	return body, nil
}

func ParseURL(raw string) (model.SkillSource, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || strings.ToLower(u.Host) != "github.com" {
		return model.SkillSource{}, fmt.Errorf("source must be an HTTPS GitHub URL")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return model.SkillSource{}, fmt.Errorf("GitHub URL must include owner and repository")
	}
	s := model.SkillSource{Type: "github", Repository: parts[0] + "/" + parts[1], Ref: "main"}
	if len(parts) == 2 {
		s.Ref = ""
		return s, nil
	}
	if len(parts) < 4 || (parts[2] != "tree" && parts[2] != "blob") {
		return model.SkillSource{}, fmt.Errorf("unsupported GitHub URL; use /tree/<ref>/<skill-path> or /blob/<ref>/SKILL.md")
	}
	s.Ref = parts[3]
	if len(parts) > 4 {
		s.Path = strings.Join(parts[4:], "/")
	}
	if parts[2] == "blob" {
		if s.Path == "" || filepath.Base(s.Path) != "SKILL.md" {
			return model.SkillSource{}, fmt.Errorf("blob URL must point to SKILL.md")
		}
		s.Path = strings.TrimSuffix(s.Path, "/SKILL.md")
	}
	return s, nil
}

func (p Provider) Resolve(ctx context.Context, source model.SkillSource) (model.SkillSource, error) {
	if source.Ref == "" {
		repoURL := fmt.Sprintf("%s/repos/%s", strings.TrimRight(p.APIBase, "/"), source.Repository)
		if body, err := p.request(ctx, http.MethodGet, repoURL); err == nil {
			var repo struct {
				DefaultBranch string `json:"default_branch"`
			}
			if json.Unmarshal(body, &repo) == nil && repo.DefaultBranch != "" {
				source.Ref = repo.DefaultBranch
			}
		}
		if source.Ref == "" {
			source.Ref = "main"
		}
	}
	commitURL := fmt.Sprintf("%s/repos/%s/commits/%s", strings.TrimRight(p.APIBase, "/"), source.Repository, url.PathEscape(source.Ref))
	body, err := p.request(ctx, http.MethodGet, commitURL)
	if err == nil {
		var c struct {
			SHA string `json:"sha"`
		}
		if json.Unmarshal(body, &c) == nil {
			source.Commit = c.SHA
		}
	}
	return source, nil
}

func (p Provider) Download(ctx context.Context, source model.SkillSource, dest string) error {
	name, cleanup, err := p.downloadArchive(ctx, source)
	if err != nil {
		return err
	}
	defer cleanup()
	_, err = extractSkillPaths(name, source.Path, dest, false)
	return err
}

func (p Provider) DownloadAll(ctx context.Context, source model.SkillSource, dest string) ([]providers.DownloadedSkill, error) {
	name, cleanup, err := p.downloadArchive(ctx, source)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	extracted, err := extractSkillPaths(name, source.Path, dest, true)
	if err != nil {
		return nil, err
	}
	result := make([]providers.DownloadedSkill, 0, len(extracted))
	for _, item := range extracted {
		itemSource := source
		itemSource.Path = item.Path
		result = append(result, providers.DownloadedSkill{Source: itemSource, Directory: item.Directory})
	}
	return result, nil
}

func (p Provider) downloadArchive(ctx context.Context, source model.SkillSource) (string, func(), error) {
	base := p.DownloadBase
	if base == "" {
		base = "https://codeload.github.com"
	}
	zipURL := fmt.Sprintf("%s/%s/zip/refs/heads/%s", strings.TrimRight(base, "/"), source.Repository, url.PathEscape(source.Ref))
	if source.Commit != "" {
		zipURL = fmt.Sprintf("%s/%s/zip/%s", strings.TrimRight(base, "/"), source.Repository, url.PathEscape(source.Commit))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, zipURL, nil)
	if err != nil {
		return "", func() {}, err
	}
	if p.Token != "" {
		req.Header.Set("Authorization", "Bearer "+p.Token)
	}
	resp, err := p.Client.Do(req)
	if err != nil {
		return "", func() {}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", func() {}, fmt.Errorf("GitHub download returned %s", resp.Status)
	}
	tmp, err := os.CreateTemp("", "cms-download-*.zip")
	if err != nil {
		return "", func() {}, err
	}
	name := tmp.Name()
	cleanup := func() { _ = os.Remove(name) }
	if _, err = io.Copy(tmp, io.LimitReader(resp.Body, 512<<20)); err != nil {
		tmp.Close()
		cleanup()
		return "", func() {}, err
	}
	if err = tmp.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return name, cleanup, nil
}

func extractSkill(zipPath, requestedPath, dest string) error {
	_, err := extractSkillPaths(zipPath, requestedPath, dest, false)
	return err
}

type extractedSkill struct {
	Path, Directory string
}

func extractSkillPaths(zipPath, requestedPath, dest string, all bool) ([]extractedSkill, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	archivePrefix := archiveRootPrefix(r.File)
	roots := map[string]*zip.File{}
	for _, f := range r.File {
		name := filepath.ToSlash(f.Name)
		if strings.HasPrefix(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "../") || name == ".." {
			return nil, fmt.Errorf("unsafe archive path %q", f.Name)
		}
		parts := strings.Split(strings.Trim(name, "/"), "/")
		for _, part := range parts {
			if part == ".." || part == "" {
				return nil, fmt.Errorf("unsafe archive path %q", f.Name)
			}
		}
		if parts[len(parts)-1] == "SKILL.md" {
			roots[strings.Join(parts[:len(parts)-1], "/")] = f
		}
	}
	var chosen []string
	if requestedPath != "" {
		var selected string
		for root := range roots {
			if root == requestedPath || strings.HasSuffix(root, "/"+requestedPath) {
				selected = root
				break
			}
		}
		if selected == "" {
			return nil, fmt.Errorf("no SKILL.md found at requested skill path %q", requestedPath)
		}
		chosen = []string{selected}
	} else if !all {
		if len(roots) == 0 {
			return nil, fmt.Errorf("repository does not contain a SKILL.md")
		}
		if len(roots) > 1 {
			candidates := make([]string, 0, len(roots))
			for root := range roots {
				candidates = append(candidates, relativeSkillPath(root, archivePrefix))
			}
			sort.Strings(candidates)
			return nil, fmt.Errorf("repository contains multiple skills; choose a specific path: %s", strings.Join(candidates, ", "))
		}
		for root := range roots {
			chosen = []string{root}
		}
	} else {
		for root := range roots {
			chosen = append(chosen, root)
		}
		sort.Strings(chosen)
		if len(chosen) == 0 {
			return nil, fmt.Errorf("repository does not contain a SKILL.md")
		}
	}
	result := make([]extractedSkill, 0, len(chosen))
	for _, root := range chosen {
		prefix := root + "/"
		sourcePath := relativeSkillPath(root, archivePrefix)
		if all && !isTopLevelSkillPath(sourcePath) {
			continue
		}
		targetDir := dest
		if all && sourcePath != "" {
			targetDir = filepath.Join(dest, filepath.FromSlash(sourcePath))
		}
		if !within(dest, targetDir) {
			return nil, fmt.Errorf("unsafe skill path %q", sourcePath)
		}
		for _, f := range r.File {
			name := filepath.ToSlash(f.Name)
			if all && belongsToOtherSkill(name, root, chosen) {
				continue
			}
			idx := strings.Index(name, prefix)
			if idx < 0 || !strings.HasPrefix(name[idx:], prefix) {
				continue
			}
			rel := strings.TrimPrefix(name[idx:], prefix)
			if rel == "" {
				continue
			}
			target := filepath.Join(targetDir, filepath.FromSlash(rel))
			if !within(targetDir, target) {
				return nil, fmt.Errorf("unsafe archive path %q", f.Name)
			}
			if f.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("archive contains unsupported symlink %q", f.Name)
			}
			if strings.HasSuffix(f.Name, "/") {
				if err := os.MkdirAll(target, 0o755); err != nil {
					return nil, err
				}
				continue
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return nil, err
			}
			in, err := f.Open()
			if err != nil {
				return nil, err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, f.Mode().Perm())
			if err == nil {
				_, err = io.Copy(out, io.LimitReader(in, 512<<20))
				out.Close()
			}
			in.Close()
			if err != nil {
				return nil, err
			}
		}
		result = append(result, extractedSkill{Path: sourcePath, Directory: targetDir})
	}
	return result, nil
}

func archiveRootPrefix(files []*zip.File) string {
	for _, f := range files {
		name := filepath.ToSlash(f.Name)
		if i := strings.IndexByte(name, '/'); i >= 0 {
			return name[:i+1]
		}
	}
	return ""
}

func relativeSkillPath(root, archivePrefix string) string {
	archiveRoot := strings.TrimSuffix(archivePrefix, "/")
	if archiveRoot != "" && root == archiveRoot {
		return ""
	}
	return strings.TrimPrefix(root, archivePrefix)
}

func belongsToOtherSkill(name, current string, selected []string) bool {
	for _, other := range selected {
		if other != current && strings.HasPrefix(other, current+"/") && strings.HasPrefix(name, other+"/") {
			return true
		}
	}
	return false
}

func within(base, target string) bool {
	b, _ := filepath.Abs(base)
	t, _ := filepath.Abs(target)
	rel, err := filepath.Rel(b, t)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (p Provider) Search(ctx context.Context, query string, limit int) ([]Candidate, error) {
	if limit <= 0 {
		limit = 30
	}
	if cached, ok := p.readCache(query, limit); ok {
		return cached, nil
	}
	q := url.QueryEscape(strings.TrimSpace(query) + " path:skills filename:SKILL.md")
	endpoint := fmt.Sprintf("%s/search/code?q=%s&per_page=%d", strings.TrimRight(p.APIBase, "/"), q, limit)
	body, err := p.request(ctx, http.MethodGet, endpoint)
	if err != nil {
		var apiErr *APIError
		if p.Token == "" && errors.As(err, &apiErr) && apiErr.Code == http.StatusUnauthorized {
			return p.searchPublicRepositories(ctx, query, limit)
		}
		return nil, err
	}
	var result struct {
		Items []struct {
			Path       string `json:"path"`
			Repository struct {
				FullName      string `json:"full_name"`
				DefaultBranch string `json:"default_branch"`
				License       struct {
					SPDX string `json:"spdx_id"`
				} `json:"license"`
			} `json:"repository"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	out := make([]Candidate, 0, len(result.Items))
	seen := map[string]bool{}
	for _, item := range result.Items {
		path, ok := skillDirectory(item.Path)
		if !ok || !isTopLevelSkillPath(path) {
			continue
		}
		ref := item.Repository.DefaultBranch
		if ref == "" {
			ref = "main"
		}
		key := item.Repository.FullName + "\x00" + path + "\x00" + ref
		if seen[key] {
			continue
		}
		seen[key] = true
		source := model.SkillSource{Type: "github", Repository: item.Repository.FullName, Path: path, Ref: ref}
		rawBase := p.RawBase
		if rawBase == "" {
			rawBase = "https://raw.githubusercontent.com"
		}
		raw := rawSkillURL(rawBase, source.Repository, ref, path)
		manifestBody, e := p.request(ctx, http.MethodGet, raw)
		if e != nil {
			continue
		}
		manifest, e := skills.ParseManifest(manifestBody)
		if e != nil {
			continue
		}
		out = append(out, Candidate{Name: manifest.Name, Description: manifest.Description, Repository: source.Repository, Path: path, Ref: ref, License: item.Repository.License.SPDX, Score: score(strings.ToLower(query), manifest.Name, path, manifest.Description)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	p.writeCache(query, limit, out)
	return out, nil
}

// Code Search is authenticated-only on some GitHub API configurations. This
// fallback uses public repository and tree endpoints and intentionally limits
// traversal so an unauthenticated search remains predictable.
func (p Provider) searchPublicRepositories(ctx context.Context, query string, limit int) ([]Candidate, error) {
	// Repository search is only a fallback for installations where Code Search
	// is unavailable without authentication. Do not add mandatory terms here:
	// GitHub combines terms with AND, so adding "agent skills" made otherwise
	// useful queries such as "go testing" return no repositories.
	q := url.QueryEscape(strings.TrimSpace(query))
	endpoint := fmt.Sprintf("%s/search/repositories?q=%s&per_page=%d", strings.TrimRight(p.APIBase, "/"), q, min(limit, 10))
	body, err := p.request(ctx, http.MethodGet, endpoint)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Items []struct {
			FullName      string `json:"full_name"`
			DefaultBranch string `json:"default_branch"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	var out []Candidate
	seen := map[string]bool{}
	for _, repo := range raw.Items {
		ref := repo.DefaultBranch
		if ref == "" {
			ref = "main"
		}
		treeURL := fmt.Sprintf("%s/repos/%s/git/trees/%s?recursive=1", strings.TrimRight(p.APIBase, "/"), repo.FullName, url.PathEscape(ref))
		treeBody, treeErr := p.request(ctx, http.MethodGet, treeURL)
		if treeErr != nil {
			continue
		}
		var tree struct {
			Tree []struct {
				Path string `json:"path"`
				Type string `json:"type"`
			} `json:"tree"`
		}
		if json.Unmarshal(treeBody, &tree) != nil {
			continue
		}
		for _, entry := range tree.Tree {
			if entry.Type != "blob" || len(out) >= limit {
				continue
			}
			path, ok := skillDirectory(entry.Path)
			if !ok || !isTopLevelSkillPath(path) {
				continue
			}
			key := repo.FullName + "\x00" + path + "\x00" + ref
			if seen[key] {
				continue
			}
			rawBase := p.RawBase
			if rawBase == "" {
				rawBase = "https://raw.githubusercontent.com"
			}
			manifestBody, fetchErr := p.request(ctx, http.MethodGet, rawSkillURL(rawBase, repo.FullName, ref, path))
			if fetchErr != nil {
				continue
			}
			manifest, parseErr := skills.ParseManifest(manifestBody)
			if parseErr != nil {
				continue
			}
			seen[key] = true
			out = append(out, Candidate{Name: manifest.Name, Description: manifest.Description, Repository: repo.FullName, Path: path, Ref: ref, Score: score(strings.ToLower(query), manifest.Name, path, manifest.Description)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	p.writeCache(query, limit, out)
	return out, nil
}

func skillDirectory(path string) (string, bool) {
	if path == "SKILL.md" {
		return "", true
	}
	if strings.HasSuffix(path, "/SKILL.md") {
		return strings.TrimSuffix(path, "/SKILL.md"), true
	}
	return "", false
}

func isTopLevelSkillPath(path string) bool {
	parts := strings.Split(path, "/")
	return len(parts) == 2 && parts[0] == "skills" && parts[1] != ""
}

func rawSkillURL(base, repository, ref, path string) string {
	base = strings.TrimRight(base, "/")
	if path == "" {
		return fmt.Sprintf("%s/%s/%s/SKILL.md", base, repository, ref)
	}
	return fmt.Sprintf("%s/%s/%s/%s/SKILL.md", base, repository, ref, path)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (p Provider) cachePath(query string, limit int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s", limit, query)))
	return filepath.Join(p.CacheDir, "github", "search-"+fmt.Sprintf("%x", sum[:8])+".json")
}
func (p Provider) readCache(query string, limit int) ([]Candidate, bool) {
	if p.CacheDir == "" || p.CacheTTL <= 0 {
		return nil, false
	}
	path := p.cachePath(query, limit)
	info, err := os.Stat(path)
	if err != nil || time.Since(info.ModTime()) > p.CacheTTL {
		return nil, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var out []Candidate
	if json.Unmarshal(b, &out) != nil {
		return nil, false
	}
	return out, true
}
func (p Provider) writeCache(query string, limit int, results []Candidate) {
	if p.CacheDir == "" || p.CacheTTL <= 0 {
		return
	}
	data, err := json.Marshal(results)
	if err != nil {
		return
	}
	_ = storage.AtomicWrite(p.cachePath(query, limit), data, 0o600)
}
func score(query, name, path, description string) int {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == name {
		return 100
	}
	if strings.Contains(name, q) {
		return 80
	}
	if strings.Contains(filepath.Base(path), q) {
		return 60
	}
	if strings.Contains(strings.ToLower(description), q) {
		return 40
	}
	return 10
}
