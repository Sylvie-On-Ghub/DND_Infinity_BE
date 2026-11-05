package character

// store_test.go - tests for SQLiteStore, each on a fresh database file.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testOwner = "user-a"
	otherUser = "user-b"
	testToken = "test-token-a" // a valid session for testOwner on servers from testConfig
)

// newTestDB opens a fresh database holding the two test users.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := OpenDB(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	for _, id := range []string{testOwner, otherUser} {
		if _, err := db.Exec(`INSERT INTO users (id, discord_id, discord_username, created_at) VALUES (?, ?, ?, 1)`,
			id, "discord-"+id, "name-"+id); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	return NewSQLiteStore(newTestDB(t))
}

func sampleCharacter(id, name string, classes ...ClassLevel) Character {
	return Character{
		ID: id, Name: name, RaceID: "elf",
		Abilities:   map[Ability]AbilityScore{Str: {Score: 10}, Dex: {Score: 16, SaveProficient: true}},
		Classes:     classes,
		CurrentHP:   12,
		TempHP:      3,
		Inspiration: 1,
		Skills:      map[string]ProficiencyLevel{"stealth": ProfExpert},
		Selections:  []Selection{{FeatureID: "asi4", ChoiceID: "scores", Allocated: map[string]int{"str": 2}}},
		Inventory:   []InventoryItem{{ItemID: "dagger", Quantity: 2, Equipped: true}},
		Modifiers:   []Modifier{{Target: TargetAC, Kind: ModAdd, Value: 1, SourceID: "ring"}},
		Spellbooks: map[string]SpellState{
			"class:wizard": {Cantrips: []string{"fire_bolt"}, Prepared: []string{"fireball"}},
		},
		SpellSlotsUsed:   map[int]int{1: 2},
		ResourceUsed:     map[string]int{"rage": 1},
		Acknowledgements: []Acknowledgement{{Key: "over_cap:::5", Note: "DM ok"}},
	}
}

// sameJSON compares by JSON so nil-versus-empty differences don't cause false failures.
func sameJSON(t *testing.T, got, want Character) {
	t.Helper()
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if string(g) != string(w) {
		t.Errorf("round trip changed the character:\n got  %s\n want %s", g, w)
	}
}

// sameCharacter is sameJSON ignoring the version, which the store assigns.
func sameCharacter(t *testing.T, got, want Character) {
	t.Helper()
	got.Version, want.Version = 0, 0
	sameJSON(t, got, want)
}

func TestStoreRoundTrip(t *testing.T) {
	s, ctx := newTestStore(t), context.Background()
	want := sampleCharacter("abc123", "Test", cls("rogue", 5))
	created, err := s.Create(ctx, testOwner, want)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Version != 1 {
		t.Errorf("created version = %d, want 1", created.Version)
	}
	got, err := s.Get(ctx, testOwner, "abc123")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Version != 1 {
		t.Errorf("version = %d, want 1", got.Version)
	}
	sameCharacter(t, got, want)
}

func TestStoreKeepsVersionOutOfData(t *testing.T) {
	db := newTestDB(t)
	s, ctx := NewSQLiteStore(db), context.Background()
	if _, err := s.Create(ctx, testOwner, Character{ID: "c", Name: "C", Version: 7}); err != nil {
		t.Fatal(err)
	}
	var data string
	if err := db.QueryRow(`SELECT data FROM characters WHERE id = 'c'`).Scan(&data); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(data, `"version"`) {
		t.Errorf("the version column is the source of truth, but data has one too: %s", data)
	}
}

func TestStoreCreateRefusesDuplicates(t *testing.T) {
	s, ctx := newTestStore(t), context.Background()
	if _, err := s.Create(ctx, testOwner, Character{ID: "dup", Name: "First"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, testOwner, Character{ID: "dup", Name: "Second"}); !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("err = %v, want ErrAlreadyExists", err)
	}
	if got, _ := s.Get(ctx, testOwner, "dup"); got.Name != "First" {
		t.Errorf("name = %q: a refused create must not change the character", got.Name)
	}
}

func TestStoreUpdateBumpsVersion(t *testing.T) {
	s, ctx := newTestStore(t), context.Background()
	c, err := s.Create(ctx, testOwner, sampleCharacter("abc", "First", cls("rogue", 1)))
	if err != nil {
		t.Fatal(err)
	}
	c.Name = "Second"
	c.CurrentHP = 99
	updated, err := s.Update(ctx, testOwner, c)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Version != 2 {
		t.Errorf("returned version = %d, want 2", updated.Version)
	}
	got, _ := s.Get(ctx, testOwner, "abc")
	if got.Name != "Second" || got.CurrentHP != 99 || got.Version != 2 {
		t.Errorf("update did not take effect: %+v", got)
	}
	list, _ := s.List(ctx, testOwner)
	if len(list) != 1 || list[0].Name != "Second" {
		t.Errorf("list = %+v, want the one renamed character", list)
	}
}

func TestStoreVersionConflict(t *testing.T) {
	s, ctx := newTestStore(t), context.Background()
	c, err := s.Create(ctx, testOwner, Character{ID: "c", Name: "Original"})
	if err != nil {
		t.Fatal(err)
	}
	first, second := c, c
	first.Name = "First writer"
	if _, err := s.Update(ctx, testOwner, first); err != nil {
		t.Fatal(err)
	}
	second.Name = "Second writer"
	if _, err := s.Update(ctx, testOwner, second); !errors.Is(err, ErrVersionConflict) {
		t.Errorf("stale update: err = %v, want ErrVersionConflict", err)
	}
	if got, _ := s.Get(ctx, testOwner, "c"); got.Name != "First writer" || got.Version != 2 {
		t.Errorf("a stale update must change nothing: %+v", got)
	}
}

func TestStoreNotFound(t *testing.T) {
	s, ctx := newTestStore(t), context.Background()
	if _, err := s.Get(ctx, testOwner, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get missing: err = %v, want ErrNotFound", err)
	}
	if _, err := s.Update(ctx, testOwner, Character{ID: "missing", Version: 1}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Update missing: err = %v, want ErrNotFound", err)
	}
	if err := s.Delete(ctx, testOwner, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete missing: err = %v, want ErrNotFound", err)
	}
}

func TestStoreDelete(t *testing.T) {
	s, ctx := newTestStore(t), context.Background()
	if _, err := s.Create(ctx, testOwner, sampleCharacter("gone", "Gone", cls("rogue", 1))); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, testOwner, "gone"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, testOwner, "gone"); !errors.Is(err, ErrNotFound) {
		t.Errorf("after delete: err = %v, want ErrNotFound", err)
	}
}

func TestStoreInvalidIDs(t *testing.T) {
	s, ctx := newTestStore(t), context.Background()
	bad := []string{"", "../evil", "a/b", `a\b`, ".hidden", "has space", "dot.json", strings.Repeat("a", 65)}
	for _, id := range bad {
		if _, err := s.Create(ctx, testOwner, Character{ID: id, Name: "X"}); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Create(%q): err = %v, want ErrInvalidID", id, err)
		}
		if _, err := s.Update(ctx, testOwner, Character{ID: id, Version: 1}); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Update(%q): err = %v, want ErrInvalidID", id, err)
		}
		if _, err := s.Get(ctx, testOwner, id); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Get(%q): err = %v, want ErrInvalidID", id, err)
		}
		if err := s.Delete(ctx, testOwner, id); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Delete(%q): err = %v, want ErrInvalidID", id, err)
		}
	}
}

func TestStoreListIsSortedAndSummarised(t *testing.T) {
	s, ctx := newTestStore(t), context.Background()
	for _, c := range []Character{
		sampleCharacter("c3", "Zed", cls("rogue", 3)),
		sampleCharacter("c1", "alice", cls("wizard", 5), cls("rogue", 2)),
		sampleCharacter("c2", "Bob", cls("fighter", 1)),
		{ID: "c4", Name: "Newcomer", PortraitURL: "https://example.com/n.png"}, // no classes: stored as null
	} {
		if _, err := s.Create(ctx, testOwner, c); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.List(ctx, testOwner)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range list {
		names = append(names, e.Name)
	}
	if strings.Join(names, ",") != "alice,Bob,Newcomer,Zed" {
		t.Errorf("order = %v, want alice,Bob,Newcomer,Zed (case-insensitive by name)", names)
	}
	if list[0].Level != 7 || strings.Join(list[0].ClassIDs, ",") != "wizard,rogue" || list[0].ID != "c1" {
		t.Errorf("alice summary = %+v, want c1 at level 7 with classes wizard,rogue", list[0])
	}
	if list[0].UpdatedAt.IsZero() {
		t.Error("the summary should carry updated_at")
	}
	if n := list[2]; n.Level != 0 || n.ClassIDs == nil || len(n.ClassIDs) != 0 || n.PortraitURL != "https://example.com/n.png" {
		t.Errorf("classless summary = %+v, want level 0, an empty class list and the portrait", n)
	}
}

func TestStoreListIsEmptyNotNil(t *testing.T) {
	list, err := newTestStore(t).List(context.Background(), testOwner)
	if err != nil || list == nil || len(list) != 0 {
		t.Errorf("List = %#v, %v; want an empty list so the JSON is []", list, err)
	}
}

func TestStoreGetToleratesOldFormatsAndUnknownFields(t *testing.T) {
	db := newTestDB(t)
	old := `{"id":"old","name":"Old","extra_field":1,
		"classes":[{"class_id":"rogue","level":2,"hit_die":8,"rolled_hp":[8,5]}]}`
	if _, err := db.Exec(`INSERT INTO characters (owner_id, id, name, data, updated_at) VALUES (?, 'old', 'Old', ?, 1)`,
		testOwner, old); err != nil {
		t.Fatal(err)
	}
	c, err := NewSQLiteStore(db).Get(context.Background(), testOwner, "old")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(c.Classes) != 1 || c.Classes[0].Level != 2 {
		t.Errorf("classes = %+v, want one level 2 class", c.Classes)
	}
}

// ---------------------------------------------------------------------------
// Ownership
// ---------------------------------------------------------------------------

func TestStoreScopesEveryMethodToItsOwner(t *testing.T) {
	s, ctx := newTestStore(t), context.Background()
	c, err := s.Create(ctx, testOwner, Character{ID: "mine", Name: "Mine"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.Get(ctx, otherUser, "mine"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get: err = %v, want ErrNotFound", err)
	}
	stolen := c
	stolen.Name = "Stolen"
	if _, err := s.Update(ctx, otherUser, stolen); !errors.Is(err, ErrNotFound) {
		t.Errorf("Update with the right version: err = %v, want ErrNotFound", err)
	}
	stolen.Version = 99
	if _, err := s.Update(ctx, otherUser, stolen); !errors.Is(err, ErrNotFound) {
		t.Errorf("Update with a wrong version: err = %v, want ErrNotFound, not a conflict", err)
	}
	if err := s.Delete(ctx, otherUser, "mine"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete: err = %v, want ErrNotFound", err)
	}
	if list, _ := s.List(ctx, otherUser); len(list) != 0 {
		t.Errorf("List = %+v, want nothing", list)
	}
	if got, err := s.Get(ctx, testOwner, "mine"); err != nil || got.Name != "Mine" || got.Version != 1 {
		t.Errorf("the owner's character changed: %+v, %v", got, err)
	}
}

func TestStoreIDsArePerOwner(t *testing.T) {
	s, ctx := newTestStore(t), context.Background()
	if _, err := s.Create(ctx, testOwner, Character{ID: "same", Name: "A's"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, otherUser, Character{ID: "same", Name: "B's"}); err != nil {
		t.Fatalf("the id being taken by someone else must not be visible: %v", err)
	}
	a, _ := s.Get(ctx, testOwner, "same")
	b, _ := s.Get(ctx, otherUser, "same")
	if a.Name != "A's" || b.Name != "B's" {
		t.Errorf("a = %q, b = %q", a.Name, b.Name)
	}
}

func TestDeletingAUserDeletesTheirCharacters(t *testing.T) {
	db := newTestDB(t)
	s, ctx := NewSQLiteStore(db), context.Background()
	if _, err := s.Create(ctx, testOwner, Character{ID: "c", Name: "C"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM users WHERE id = ?`, testOwner); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, testOwner, "c"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want the character gone (foreign keys on)", err)
	}
}

func TestCreateForUnknownOwnerFails(t *testing.T) {
	if _, err := newTestStore(t).Create(context.Background(), "nobody", Character{ID: "c"}); err == nil {
		t.Error("foreign keys should refuse a character with no owner")
	}
}

// ---------------------------------------------------------------------------
// Opening and migrating
// ---------------------------------------------------------------------------

func TestOpenDBSetsPragmasOnEveryConnection(t *testing.T) {
	db := newTestDB(t)
	db.SetMaxOpenConns(3)
	conns := make([]*sql.Conn, 3) // hold them open together so each is a separate connection
	for i := range conns {
		c, err := db.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		conns[i] = c
	}
	for _, c := range conns {
		var fk, busy int
		var mode string
		ctx := context.Background()
		c.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk)
		c.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy)
		c.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode)
		if fk != 1 || busy != 5000 || mode != "wal" {
			t.Errorf("foreign_keys %d, busy_timeout %d, journal_mode %q; want 1, 5000, wal", fk, busy, mode)
		}
	}
}

func TestMigrationsRunOnceAndRecordTheVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "test.db")
	ctx := context.Background()
	db, err := OpenDB(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	list, err := listMigrations()
	if err != nil {
		t.Fatal(err)
	}
	var version int
	db.QueryRow("PRAGMA user_version").Scan(&version)
	if version != len(list) {
		t.Errorf("user_version = %d, want %d", version, len(list))
	}
	if _, err := db.Exec(`INSERT INTO users (id, discord_id, discord_username, created_at) VALUES ('u', '1', 'n', 1)`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	db, err = OpenDB(ctx, path) // reopening must not rerun anything: CREATE TABLE would fail
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	defer db.Close()
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	if n != 1 {
		t.Errorf("users = %d after reopening, want the data kept", n)
	}
}

func TestOpenDBRefusesANewerDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()
	db, err := OpenDB(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA user_version = 9999"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if db, err := OpenDB(ctx, path); err == nil {
		db.Close()
		t.Error("a database from a newer program should be refused")
	}
}

func TestOpenDBFileIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("permissions = %o, want 600", perm)
	}
}

func TestUniqueUsernameIgnoresCase(t *testing.T) {
	db := newTestDB(t)
	_, err := db.Exec(`INSERT INTO users (id, discord_id, discord_username, created_at) VALUES ('x', '9', 'NAME-USER-A', 1)`)
	if err == nil {
		t.Error("usernames differing only by case should collide")
	}
}

// ---------------------------------------------------------------------------
// Reading old character files
// ---------------------------------------------------------------------------

func TestReadCharacterFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	c, err := ReadCharacterFile(write("old.json", `{"id":"old","name":"Old","extra_field":1,"classes":[{"class_id":"rogue","level":2}]}`))
	if err != nil || c.ID != "old" || len(c.Classes) != 1 {
		t.Errorf("unknown fields should be ignored: %+v, %v", c, err)
	}
	c, err = ReadCharacterFile(write("noid.json", `{"name":"No Id"}`))
	if err != nil || c.ID != "noid" {
		t.Errorf("a missing id should come from the filename: %+v, %v", c, err)
	}
	if _, err := ReadCharacterFile(write("mismatch.json", `{"id":"other"}`)); err == nil {
		t.Error("an id that disagrees with the filename should be an error")
	}
	if _, err := ReadCharacterFile(write("bad.json", `{not json`)); err == nil {
		t.Error("invalid JSON should be an error")
	}
	if _, err := ReadCharacterFile(write("has space.json", `{}`)); !errors.Is(err, ErrInvalidID) {
		t.Errorf("a filename that isn't a valid id: err = %v, want ErrInvalidID", err)
	}
}

// ---------------------------------------------------------------------------
// IDs
// ---------------------------------------------------------------------------

func TestNewCharacterID(t *testing.T) {
	a, b := NewCharacterID(), NewCharacterID()
	if len(a) != 16 || a == b {
		t.Errorf("ids %q and %q should be distinct 16-character values", a, b)
	}
	if err := validID(a); err != nil {
		t.Errorf("generated id should be valid: %v", err)
	}
}
