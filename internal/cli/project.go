package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/ikts/cms/internal/model"
	"github.com/ikts/cms/internal/skills"
	"github.com/ikts/cms/internal/storage"
)

func (a *App) freeze(args []string) error {
	if len(args) < 1 {
		return fail(2, errors.New("usage: cms freeze <context>"))
	}
	if len(args) > 1 {
		return fail(2, errors.New("usage: cms freeze <context>"))
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	contextValue, err := a.Contexts.Get(args[0])
	if err != nil {
		return fail(6, err)
	}
	targets := a.defaultTargetNames()
	if state, stateErr := storage.LoadState(root); stateErr == nil && state.Context == contextValue.Name && len(state.Targets) > 0 {
		targets = append([]string(nil), state.Targets...)
	}
	manifest := model.ProjectManifest{SchemaVersion: 2, Context: contextValue.Name, Description: contextValue.Description, Targets: targets}
	for _, ref := range contextValue.Skills {
		metadata, getErr := a.Registry.Get(ref.ID)
		if getErr != nil {
			return fail(6, getErr)
		}
		if metadata.Source.Type == "" || metadata.Source.Repository == "" || metadata.Source.Commit == "" {
			return fail(3, fmt.Errorf("skill %q does not have a reproducible pinned source", metadata.ID))
		}
		manifest.Skills = append(manifest.Skills, model.PinnedSkill{ID: metadata.ID, Name: metadata.Name, Description: metadata.Description, Source: metadata.Source})
	}
	refs := contextValue.MCPRefs
	if len(refs) == 0 {
		for _, id := range contextValue.MCPs {
			refs = append(refs, model.MCPRef{ID: id})
		}
	}
	manifest.ContextMCPs = append([]model.MCPRef(nil), refs...)
	for _, ref := range refs {
		m, getErr := a.MCPs.Get(ref.ID)
		if getErr != nil {
			return fail(6, getErr)
		}
		manifest.MCPs = append(manifest.MCPs, model.PinnedMCP{Metadata: m})
		if !m.Reproducible {
			fmt.Fprintf(a.Err, "warning: MCP %q is not reproducible; its source/version is not fully pinned\n", m.Name)
		}
	}
	if err := storage.SaveManifest(root, manifest); err != nil {
		return fail(3, err)
	}
	if a.JSON {
		var warnings []string
		for _, m := range manifest.MCPs {
			if !m.Metadata.Reproducible {
				warnings = append(warnings, fmt.Sprintf("MCP %q is not reproducible", m.Metadata.Name))
			}
		}
		return jsonEncode(a.Out, struct {
			Manifest model.ProjectManifest `json:"manifest"`
			Warnings []string              `json:"warnings"`
		}{manifest, warnings})
	}
	if !a.Quiet {
		fmt.Fprintf(a.Out, "wrote %s with %d skill(s) and %d MCP(s)\n", storage.ManifestPath(root), len(manifest.Skills), len(manifest.MCPs))
	}
	return nil
}

func (a *App) syncProject(args []string) error {
	if len(args) != 0 {
		return fail(2, errors.New("usage: cms sync"))
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	manifest, err := storage.LoadManifest(root)
	if errors.Is(err, os.ErrNotExist) {
		return fail(2, errors.New("cms.toml was not found; run cms freeze <context> first"))
	}
	if err != nil {
		return fail(3, err)
	}
	installed, created, err := a.syncManifest(manifest)
	if err != nil {
		return err
	}
	if a.JSON {
		return jsonEncode(a.Out, struct {
			Context string                `json:"context"`
			Created bool                  `json:"context_created"`
			Skills  []model.SkillMetadata `json:"skills"`
			MCPs    int                   `json:"mcps"`
		}{manifest.Context, created, installed, len(manifest.MCPs)})
	}
	if !a.Quiet {
		if created {
			fmt.Fprintf(a.Out, "created context %s\n", manifest.Context)
		}
		if len(installed) == 0 {
			fmt.Fprintln(a.Out, "all manifest skills are already installed")
		} else {
			fmt.Fprintf(a.Out, "installed %d missing skill(s)\n", len(installed))
		}
		if len(manifest.MCPs) == 0 {
			fmt.Fprintln(a.Out, "no MCPs in manifest")
		} else {
			fmt.Fprintf(a.Out, "synchronized %d MCP(s) from manifest\n", len(manifest.MCPs))
		}
	}
	return nil
}

func (a *App) syncManifest(manifest model.ProjectManifest) ([]model.SkillMetadata, bool, error) {
	newIDs := []string{}
	newMCPIDs := []string{}
	installed := []model.SkillMetadata{}
	rollback := func() {
		for _, id := range newIDs {
			_ = a.Registry.Remove(id)
		}
		for _, id := range newMCPIDs {
			_ = a.MCPs.Remove(id)
		}
	}
	for _, pinned := range manifest.Skills {
		expectedID := skills.SkillID(pinned.Name, pinned.Source)
		if expectedID != pinned.ID {
			rollback()
			return nil, false, fail(3, fmt.Errorf("skill %q has an ID that does not match its pinned source", pinned.ID))
		}
		metadata, getErr := a.Registry.Get(pinned.ID)
		if getErr == nil {
			if metadata.Name != pinned.Name || metadata.Source != pinned.Source {
				rollback()
				return nil, false, fail(3, fmt.Errorf("installed skill %q does not match cms.toml", pinned.ID))
			}
			if info, statErr := os.Stat(a.Registry.Store.ContentPath(pinned.ID)); statErr != nil || !info.IsDir() {
				rollback()
				return nil, false, fail(3, fmt.Errorf("skill %q has metadata but its content is missing", pinned.ID))
			}
			continue
		}

		tmp, tempErr := os.MkdirTemp("", "cms-sync-skill-")
		if tempErr != nil {
			rollback()
			return nil, false, fail(1, tempErr)
		}
		downloadErr := a.Provider.Download(context.Background(), pinned.Source, tmp)
		if downloadErr != nil {
			_ = os.RemoveAll(tmp)
			rollback()
			return nil, false, fail(5, fmt.Errorf("could not download skill %q: %w", pinned.ID, downloadErr))
		}
		metadata, _, installErr := a.Registry.InstallDirectory(tmp, pinned.Source)
		_ = os.RemoveAll(tmp)
		if installErr != nil {
			rollback()
			return nil, false, fail(3, fmt.Errorf("could not install skill %q: %w", pinned.ID, installErr))
		}
		if metadata.ID != pinned.ID || metadata.Name != pinned.Name {
			rollback()
			return nil, false, fail(3, fmt.Errorf("downloaded skill does not match cms.toml entry %q", pinned.ID))
		}
		newIDs = append(newIDs, metadata.ID)
		installed = append(installed, metadata)
	}
	for _, pinned := range manifest.MCPs {
		if pinned.Metadata.ID == "" || pinned.Metadata.CanonicalID() != pinned.Metadata.ID {
			rollback()
			return nil, false, fail(3, fmt.Errorf("MCP %q has an ID that does not match cms.toml", pinned.Metadata.ID))
		}
		existing, getErr := a.MCPs.Get(pinned.Metadata.ID)
		if getErr == nil {
			if existing.CanonicalID() != pinned.Metadata.CanonicalID() {
				rollback()
				return nil, false, fail(3, fmt.Errorf("installed MCP %q does not match cms.toml", pinned.Metadata.ID))
			}
		} else {
			if _, _, regErr := a.MCPs.Register(pinned.Metadata); regErr != nil {
				rollback()
				return nil, false, fail(3, regErr)
			}
			newMCPIDs = append(newMCPIDs, pinned.Metadata.ID)
		}
	}

	desired := model.Context{Name: manifest.Context, Description: manifest.Description}
	for _, pinned := range manifest.Skills {
		desired.Skills = append(desired.Skills, model.SkillRef{ID: pinned.ID})
	}
	desired.SchemaVersion = 2
	desired.MCPRefs = append([]model.MCPRef(nil), manifest.ContextMCPs...)
	for _, r := range desired.MCPRefs {
		desired.MCPs = append(desired.MCPs, r.ID)
	}
	contextPath := a.Contexts.Path(manifest.Context)
	current, contextErr := a.Contexts.Get(manifest.Context)
	if contextErr == nil {
		if !sameContext(current, desired) {
			rollback()
			return nil, false, fail(3, fmt.Errorf("context %q differs from cms.toml; update the manifest with cms freeze or resolve it manually", manifest.Context))
		}
		return installed, false, nil
	}
	if _, statErr := os.Stat(contextPath); !os.IsNotExist(statErr) {
		rollback()
		return nil, false, fail(3, contextErr)
	}
	if saveErr := a.Contexts.Save(desired); saveErr != nil {
		rollback()
		return nil, false, saveErr
	}
	return installed, true, nil
}

func sameContext(a, b model.Context) bool {
	if a.Name != b.Name || a.Description != b.Description || len(a.Skills) != len(b.Skills) || len(a.MCPRefs) != len(b.MCPRefs) {
		return false
	}
	for i := range a.Skills {
		if a.Skills[i].ID != b.Skills[i].ID {
			return false
		}
	}
	for i := range a.MCPRefs {
		if a.MCPRefs[i].ID != b.MCPRefs[i].ID || a.MCPRefs[i].Alias != b.MCPRefs[i].Alias || !sameStrings(a.MCPRefs[i].Tools.Allow, b.MCPRefs[i].Tools.Allow) || !sameStrings(a.MCPRefs[i].Tools.Deny, b.MCPRefs[i].Tools.Deny) {
			return false
		}
	}
	return true
}
func sameStrings(a, b []string) bool {
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

func jsonEncode(out io.Writer, value any) error {
	return json.NewEncoder(out).Encode(value)
}
