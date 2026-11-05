package character

// users_test.go - managing accounts and importing old character files.

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAddAndListUsers(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	u, err := AddUser(ctx, db, "123456789", "Mira")
	if err != nil {
		t.Fatal(err)
	}
	if len(u.ID) != 32 || u.DiscordID != "123456789" || u.DiscordUsername != "Mira" {
		t.Errorf("user = %+v", u)
	}
	found, err := UserByUsername(ctx, db, "mira")
	if err != nil || found.ID != u.ID || found.DiscordUsername != "Mira" {
		t.Errorf("lookup ignoring case = %+v, %v", found, err)
	}
	list, err := ListUsers(ctx, db)
	if err != nil || len(list) != 3 { // the two test users, plus Mira
		t.Errorf("list = %+v, %v", list, err)
	}
}

func TestAddUserRefusesBadInput(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	if _, err := AddUser(ctx, db, "111", "taken"); err != nil {
		t.Fatal(err)
	}
	if _, err := AddUser(ctx, db, "222", "TAKEN"); !errors.Is(err, ErrUserExists) {
		t.Errorf("duplicate username: err = %v", err)
	}
	if _, err := AddUser(ctx, db, "111", "other"); !errors.Is(err, ErrUserExists) {
		t.Errorf("duplicate Discord id: err = %v", err)
	}
	if _, err := AddUser(ctx, db, "mira", "123"); err == nil {
		t.Error("swapped arguments should be refused: a Discord id is numeric")
	}
	if _, err := AddUser(ctx, db, "333", "  "); err == nil {
		t.Error("a blank username should be refused")
	}
}

func TestRenameUser(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	u, _ := AddUser(ctx, db, "111", "old")
	if _, err := AddUser(ctx, db, "222", "other"); err != nil {
		t.Fatal(err)
	}
	if err := RenameUser(ctx, db, "OLD", "new"); err != nil {
		t.Fatal(err)
	}
	if got, err := UserByUsername(ctx, db, "new"); err != nil || got.ID != u.ID {
		t.Errorf("after rename: %+v, %v", got, err)
	}
	if _, err := UserByUsername(ctx, db, "old"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("the old name should be free: %v", err)
	}
	if err := RenameUser(ctx, db, "new", "other"); !errors.Is(err, ErrUserExists) {
		t.Errorf("renaming onto a taken name: err = %v", err)
	}
	if err := RenameUser(ctx, db, "new", "NEW"); err != nil {
		t.Errorf("changing only the case: %v", err)
	}
	if err := RenameUser(ctx, db, "nobody", "x"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("renaming nobody: err = %v", err)
	}
}

func TestRemoveUserTakesEverythingWithThem(t *testing.T) {
	cfg, store := testConfig(t)
	db, ctx := cfg.Auth.db, context.Background()
	saveFixture(t, store, Character{ID: "c", Name: "C"})
	srv := NewServer(cfg)

	if n, err := CountCharacters(ctx, db, testOwner); err != nil || n != 1 {
		t.Errorf("count = %d, %v", n, err)
	}
	if err := RemoveUser(ctx, db, testOwner); err != nil {
		t.Fatal(err)
	}
	wantError(t, call(t, srv, http.MethodGet, "/api/me", nil), http.StatusUnauthorized, "unauthorized")
	if n, _ := CountCharacters(ctx, db, testOwner); n != 0 {
		t.Errorf("%d characters left", n)
	}
	if err := RemoveUser(ctx, db, testOwner); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("removing twice: err = %v", err)
	}
}

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestImportCharacters(t *testing.T) {
	store, ctx := newTestStore(t), context.Background()
	dir := writeFiles(t, map[string]string{
		"temi.json":     `{"id":"temi","name":"Temi","classes":[{"class_id":"warlock","level":3}]}`,
		"noid.json":     `{"name":"No Id"}`,
		"bad.json":      `{not json`,
		"mismatch.json": `{"id":"other"}`,
		"badurl.json":   `{"portrait_url":"javascript:alert(1)"}`,
		".tmp-x.json":   `{}`,
		"notes.txt":     `ignored`,
	})
	modified := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(dir, "temi.json"), modified, modified); err != nil {
		t.Fatal(err)
	}

	report, err := ImportCharacters(ctx, store, testOwner, dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(report.Imported, ",") != "noid,temi" || len(report.Skipped) != 0 || len(report.Failed) != 3 {
		t.Errorf("report = %+v, want noid and temi imported and 3 failures", report)
	}
	list, _ := store.List(ctx, testOwner)
	if len(list) != 2 || list[1].ID != "temi" || list[1].Level != 3 {
		t.Fatalf("list = %+v", list)
	}
	if !list[1].UpdatedAt.Equal(modified) {
		t.Errorf("updated_at = %v, want the file's time %v", list[1].UpdatedAt, modified)
	}
	if c, err := store.Get(ctx, testOwner, "temi"); err != nil || c.Version != 1 {
		t.Errorf("imported character = %+v, %v; want version 1", c, err)
	}

	if _, err := ImportCharacters(ctx, store, testOwner, filepath.Join(dir, "missing")); err == nil {
		t.Error("a missing folder should be an error")
	}
}

func TestImportIsSafeToRerun(t *testing.T) {
	store, ctx := newTestStore(t), context.Background()
	dir := writeFiles(t, map[string]string{"temi.json": `{"id":"temi","name":"From file"}`})
	if _, err := ImportCharacters(ctx, store, testOwner, dir); err != nil {
		t.Fatal(err)
	}

	// The character moves on after the import, and a second run must not undo that.
	c, _ := store.Get(ctx, testOwner, "temi")
	c.Name = "Changed in play"
	if _, err := store.Update(ctx, testOwner, c); err != nil {
		t.Fatal(err)
	}

	report, err := ImportCharacters(ctx, store, testOwner, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 0 || strings.Join(report.Skipped, ",") != "temi" || len(report.Failed) != 0 {
		t.Errorf("report = %+v, want temi skipped", report)
	}
	if got, _ := store.Get(ctx, testOwner, "temi"); got.Name != "Changed in play" || got.Version != 2 {
		t.Errorf("re-running changed the character: %+v", got)
	}

	// Someone else having the same id is no obstacle.
	report, err = ImportCharacters(ctx, store, otherUser, dir)
	if err != nil || strings.Join(report.Imported, ",") != "temi" {
		t.Errorf("for another owner: %+v, %v", report, err)
	}
}
