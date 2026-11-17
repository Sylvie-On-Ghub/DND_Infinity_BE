package character

// backup_test.go - tests for BackupDB.

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

var backupDay = time.Date(2026, 11, 17, 3, 30, 0, 0, time.Local)

func addCharacter(t *testing.T, s *SQLiteStore, id string) {
	t.Helper()
	if _, err := s.Create(context.Background(), testOwner, Character{ID: id, Name: id}); err != nil {
		t.Fatal(err)
	}
}

func countSnapshotCharacters(t *testing.T, path string) int {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM characters`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestBackupDBWritesVerifiedSnapshot(t *testing.T) {
	s := newTestStore(t)
	addCharacter(t, s, "one")
	dir := t.TempDir()

	path, err := BackupDB(context.Background(), s.db, dir, 7, backupDay)
	if err != nil {
		t.Fatalf("BackupDB: %v", err)
	}
	if want := filepath.Join(dir, "dnd-2026-11-17.db"); path != want {
		t.Errorf("path = %s, want %s", path, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o640 {
		t.Errorf("mode = %o, want 640", mode)
	}
	if n := countSnapshotCharacters(t, path); n != 1 {
		t.Errorf("snapshot has %d characters, want 1", n)
	}
	for _, name := range dirNames(t, dir) {
		if strings.HasPrefix(name, ".") {
			t.Errorf("temporary file left behind: %s", name)
		}
	}
}

func TestBackupDBIncludesWALRows(t *testing.T) {
	s := newTestStore(t)
	addCharacter(t, s, "one")
	addCharacter(t, s, "two")

	path, err := BackupDB(context.Background(), s.db, t.TempDir(), 7, backupDay)
	if err != nil {
		t.Fatalf("BackupDB: %v", err)
	}
	if n := countSnapshotCharacters(t, path); n != 2 {
		t.Errorf("snapshot has %d characters, want 2", n)
	}
}

func TestBackupDBSameDayReplaces(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	addCharacter(t, s, "one")
	if _, err := BackupDB(context.Background(), s.db, dir, 7, backupDay); err != nil {
		t.Fatalf("first BackupDB: %v", err)
	}
	addCharacter(t, s, "two")
	path, err := BackupDB(context.Background(), s.db, dir, 7, backupDay.Add(time.Hour))
	if err != nil {
		t.Fatalf("second BackupDB: %v", err)
	}
	if names := dirNames(t, dir); len(names) != 1 {
		t.Errorf("files = %v, want just one", names)
	}
	if n := countSnapshotCharacters(t, path); n != 2 {
		t.Errorf("snapshot has %d characters, want 2", n)
	}
}

func TestBackupDBPrunesOnlySnapshots(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	others := []string{"notes.txt", ".dnd-old.tmp"}
	for day := 1; day <= 10; day++ {
		others = append(others, time.Date(2026, 11, day, 0, 0, 0, 0, time.Local).Format("dnd-2006-01-02.db"))
	}
	for _, name := range others {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o640); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := BackupDB(context.Background(), s.db, dir, 7, backupDay); err != nil {
		t.Fatalf("BackupDB: %v", err)
	}
	want := []string{".dnd-old.tmp", "dnd-2026-11-05.db", "dnd-2026-11-06.db", "dnd-2026-11-07.db",
		"dnd-2026-11-08.db", "dnd-2026-11-09.db", "dnd-2026-11-10.db", "dnd-2026-11-17.db", "notes.txt"}
	if got := dirNames(t, dir); !slices.Equal(got, want) {
		t.Errorf("files = %v, want %v", got, want)
	}
}

func TestBackupDBMissingDir(t *testing.T) {
	s := newTestStore(t)
	dir := filepath.Join(t.TempDir(), "missing")
	if _, err := BackupDB(context.Background(), s.db, dir, 7, backupDay); err == nil {
		t.Fatal("BackupDB into a missing folder: want an error")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("folder was created: %v", err)
	}
}
