package character

// backup.go - nightly snapshots of the database.
//
// VACUUM INTO writes a consistent, compacted copy through SQLite itself, so it sees rows still in
// the WAL; copying dnd.db on disk would not. The copy is written under a dot-name, checked, and only
// then renamed to dnd-YYYY-MM-DD.db, so a half-written or corrupt snapshot never looks like a backup.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"
)

var snapshotName = regexp.MustCompile(`^dnd-\d{4}-\d{2}-\d{2}\.db$`)

// BackupDB writes a verified snapshot of db to dir as dnd-<date of now>.db, replacing one from the
// same day, then deletes all but the newest keep snapshots. It returns the snapshot's path. dir
// must already exist.
func BackupDB(ctx context.Context, db *sql.DB, dir string, keep int, now time.Time) (string, error) {
	if info, err := os.Stat(dir); err != nil {
		return "", fmt.Errorf("backup folder: %w", err)
	} else if !info.IsDir() {
		return "", fmt.Errorf("backup folder %s is not a folder", dir)
	}

	// VACUUM INTO refuses a file that exists, so the name is chosen but not created.
	tmp := filepath.Join(dir, fmt.Sprintf(".dnd-backup-%d.db", time.Now().UnixNano()))
	path := filepath.Join(dir, "dnd-"+now.Format(time.DateOnly)+".db")
	if err := writeSnapshot(ctx, db, tmp); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("backup: %w", err)
	}
	if err := pruneSnapshots(dir, keep); err != nil {
		return path, err
	}
	return path, nil
}

func writeSnapshot(ctx context.Context, db *sql.DB, tmp string) error {
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, tmp); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	if err := checkIntegrity(ctx, tmp); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o640); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	return nil
}

func checkIntegrity(ctx context.Context, path string) error {
	snap, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return fmt.Errorf("check backup: %w", err)
	}
	defer snap.Close()
	rows, err := snap.QueryContext(ctx, `PRAGMA integrity_check`)
	if err != nil {
		return fmt.Errorf("check backup: %w", err)
	}
	defer rows.Close()
	var results []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return fmt.Errorf("check backup: %w", err)
		}
		results = append(results, s)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("check backup: %w", err)
	}
	if len(results) != 1 || results[0] != "ok" {
		return fmt.Errorf("backup failed its integrity check: %v", results)
	}
	return nil
}

// pruneSnapshots deletes all but the newest keep snapshots. The date in the name sorts as text, and
// other files in dir are left alone.
func pruneSnapshots(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("prune backups: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() && snapshotName.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	var errs []error
	for len(names) > keep {
		if err := os.Remove(filepath.Join(dir, names[0])); err != nil {
			errs = append(errs, err)
		}
		names = names[1:]
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("prune backups: %w", err)
	}
	return nil
}
