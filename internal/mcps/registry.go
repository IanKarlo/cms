// Package mcps contains the MCP domain-facing registry. The storage package
// owns the on-disk format; this package keeps callers independent of it.
package mcps

import (
	"fmt"
	"time"

	"github.com/ikts/cms/internal/model"
	"github.com/ikts/cms/internal/storage"
)

type Registry struct{ Store storage.MCPRegistry }

func NewRegistry(paths storage.Paths) Registry {
	return Registry{Store: storage.MCPRegistry{Paths: paths}}
}
func (r Registry) Get(id string) (model.MCPMetadata, error) { return r.Store.Get(id) }
func (r Registry) List() ([]model.MCPMetadata, error)       { return r.Store.List() }
func (r Registry) Remove(id string) error                   { return r.Store.Remove(id) }

func (r Registry) Register(m model.MCPMetadata) (model.MCPMetadata, bool, error) {
	if err := storage.ValidateMCP(m); err != nil {
		return m, false, err
	}
	if m.ID == "" {
		m.ID = m.CanonicalID()
	}
	if m.RegisteredAt.IsZero() {
		m.RegisteredAt = time.Now().UTC()
	}
	if existing, err := r.Get(m.ID); err == nil {
		if existing.CanonicalID() == m.CanonicalID() {
			return existing, true, nil
		}
		return m, false, fmt.Errorf("MCP %q already contains a different definition", m.ID)
	}
	if err := r.Store.Save(m); err != nil {
		return m, false, err
	}
	return m, false, nil
}
