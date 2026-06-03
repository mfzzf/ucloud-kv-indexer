package config

import (
	"encoding/json"
	"log"
	"os"
)

// FilePersister stores the config snapshot as a pretty-printed JSON file,
// written atomically via a temp file + rename. This is the default backend and
// preserves the original on-disk format.
type FilePersister struct {
	path string
}

// NewFilePersister returns a persister writing to the given path.
func NewFilePersister(path string) *FilePersister {
	return &FilePersister{path: path}
}

// Save writes the snapshot atomically. Errors are logged, not returned, so a
// transient disk problem never blocks a config mutation.
func (f *FilePersister) Save(snap Snapshot) {
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		log.Printf("config: marshal snapshot: %v", err)
		return
	}
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		log.Printf("config: write %s: %v", tmp, err)
		return
	}
	if err := os.Rename(tmp, f.path); err != nil {
		log.Printf("config: rename %s -> %s: %v", tmp, f.path, err)
	}
}

// Load reads the JSON snapshot. A missing file means "fresh" (ok=false, no error).
func (f *FilePersister) Load() (Snapshot, bool, error) {
	b, err := os.ReadFile(f.path)
	if err != nil {
		if os.IsNotExist(err) {
			return Snapshot{}, false, nil
		}
		return Snapshot{}, false, err
	}
	var snap Snapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return Snapshot{}, false, err
	}
	return snap, true, nil
}
