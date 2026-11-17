package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	character "Users/sylviemckernan/Documents/Projects/DND"
)

func TestBackupMissingDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "dnd.db")
	t.Setenv("DND_DB_PATH", dbPath)
	dir := t.TempDir()

	err := run(context.Background(), []string{"backup", dir})
	if err == nil || !strings.Contains(err.Error(), dbPath) {
		t.Fatalf("err = %v, want one naming %s", err, dbPath)
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Errorf("database was created: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("backup folder has %d files, want none", len(entries))
	}
}

func TestBackupCommand(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "dnd.db")
	t.Setenv("DND_DB_PATH", dbPath)
	db, err := character.OpenDB(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := character.AddUser(context.Background(), db, "123", "someone"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	dir := t.TempDir()

	if err := run(context.Background(), []string{"backup", dir}); err != nil {
		t.Fatalf("backup: %v", err)
	}
	snaps, err := filepath.Glob(filepath.Join(dir, "dnd-*.db"))
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 1 {
		t.Errorf("snapshots = %v, want exactly one", snaps)
	}
}
