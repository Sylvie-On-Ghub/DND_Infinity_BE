package character

// import.go - copying the JSON character files older versions saved into the database.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ImportFailure is a file that couldn't be imported.
type ImportFailure struct {
	File string
	Err  error
}

// ImportReport says what became of each file.
type ImportReport struct {
	Imported []string // character ids
	Skipped  []string // character ids the owner already has, left untouched
	Failed   []ImportFailure
}

// ImportCharacters adds every <id>.json in dir to the owner's characters. It is safe to run again:
// an id the owner already has is skipped, so nothing they've changed since is overwritten. One bad
// file doesn't stop the others. A character's updated_at comes from its file's modification time.
func ImportCharacters(ctx context.Context, store *SQLiteStore, ownerID, dir string) (ImportReport, error) {
	var report ImportReport
	entries, err := os.ReadDir(dir)
	if err != nil {
		return report, fmt.Errorf("read character directory: %w", err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".json") {
			continue
		}
		fail := func(err error) { report.Failed = append(report.Failed, ImportFailure{File: name, Err: err}) }

		info, err := e.Info()
		if err != nil {
			fail(err)
			continue
		}
		c, err := ReadCharacterFile(filepath.Join(dir, name))
		if err != nil {
			fail(err)
			continue
		}
		inserted, err := store.insert(ctx, ownerID, c, info.ModTime())
		switch {
		case err != nil:
			fail(err)
		case inserted:
			report.Imported = append(report.Imported, c.ID)
		default:
			report.Skipped = append(report.Skipped, c.ID)
		}
	}
	return report, nil
}
