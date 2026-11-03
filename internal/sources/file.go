package sources

// This file reads and writes <configDir>/sources.json, the only place external
// source definitions are stored. Definitions never go into the DSL, the
// internal store, or Room presets, because those are shared or rendered.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/yacchi/statusloom/internal/store"
)

// File is the on-disk shape of sources.json. Sources is keyed by source id.
type File struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Sources       map[string]Definition `json:"sources"`
}

// Path returns <configDir>/sources.json. The file need not exist.
func Path() (string, error) {
	dir, err := store.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sources.json"), nil
}

// Load reads sources.json. A missing file means no sources and is not an error.
// Each Definition.ID is filled from its map key, because the key is the only
// place the id is stored on disk.
func Load() (File, error) {
	p, err := Path()
	if err != nil {
		return File{}, err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return File{SchemaVersion: SchemaVersion, Sources: map[string]Definition{}}, nil
	}
	if err != nil {
		return File{}, fmt.Errorf("sources: read %s: %w", p, err)
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return File{}, fmt.Errorf("sources: parse %s: %w", p, err)
	}
	if f.Sources == nil {
		f.Sources = map[string]Definition{}
	}
	for id, d := range f.Sources {
		d.ID = id
		f.Sources[id] = d
	}
	return f, nil
}

// Save validates every definition in key order, with ID set to its key. The
// first error is returned and nothing is written. Otherwise it writes the file
// atomically with SchemaVersion forced to SchemaVersion (1).
func Save(f File) error {
	sources := make(map[string]Definition, len(f.Sources))
	for _, id := range f.IDs() {
		d := f.Sources[id]
		d.ID = id
		if err := d.Validate(); err != nil {
			return err
		}
		sources[id] = d
	}
	out := File{SchemaVersion: SchemaVersion, Sources: sources}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("sources: encode: %w", err)
	}
	data = append(data, '\n')
	p, err := Path()
	if err != nil {
		return err
	}
	return store.WriteFileAtomic(p, data)
}

// Get returns the definition for id, or a zero Definition and false.
func (f File) Get(id string) (Definition, bool) {
	d, ok := f.Sources[id]
	return d, ok
}

// Put validates d, including d.ID, before touching the map. It then inserts or
// replaces the definition under d.ID. A nil map is allocated first.
func (f *File) Put(d Definition) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if f.Sources == nil {
		f.Sources = map[string]Definition{}
	}
	f.Sources[d.ID] = d
	return nil
}

// Remove deletes id and reports whether it was present.
func (f *File) Remove(id string) bool {
	if _, ok := f.Sources[id]; !ok {
		return false
	}
	delete(f.Sources, id)
	return true
}

// IDs returns the source ids in sorted order.
func (f File) IDs() []string {
	ids := make([]string, 0, len(f.Sources))
	for id := range f.Sources {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
