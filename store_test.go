package character

// store_test.go - tests for FileStore and MemoryStore. Both run the same suite, so they
// are held to the same behaviour. Written without being able to run them.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func runStoreSuite(t *testing.T, newStore func(t *testing.T) CharacterStore) {
	t.Run("round trip", func(t *testing.T) {
		s := newStore(t)
		want := sampleCharacter("abc123", "Test", cls("rogue", 5))
		if err := s.Save(want); err != nil {
			t.Fatalf("Save: %v", err)
		}
		got, err := s.Get("abc123")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		sameJSON(t, got, want)
	})

	t.Run("overwrite", func(t *testing.T) {
		s := newStore(t)
		c := sampleCharacter("abc", "First", cls("rogue", 1))
		if err := s.Save(c); err != nil {
			t.Fatal(err)
		}
		c.Name = "Second"
		c.CurrentHP = 99
		if err := s.Save(c); err != nil {
			t.Fatal(err)
		}
		got, _ := s.Get("abc")
		if got.Name != "Second" || got.CurrentHP != 99 {
			t.Errorf("second save did not replace the first: %+v", got)
		}
		list, _ := s.List()
		if len(list) != 1 {
			t.Errorf("overwriting should not create a second character, list = %+v", list)
		}
	})

	t.Run("not found", func(t *testing.T) {
		s := newStore(t)
		if _, err := s.Get("missing"); !errors.Is(err, ErrNotFound) {
			t.Errorf("Get missing: err = %v, want ErrNotFound", err)
		}
		if err := s.Delete("missing"); !errors.Is(err, ErrNotFound) {
			t.Errorf("Delete missing: err = %v, want ErrNotFound", err)
		}
	})

	t.Run("delete", func(t *testing.T) {
		s := newStore(t)
		if err := s.Save(sampleCharacter("gone", "Gone", cls("rogue", 1))); err != nil {
			t.Fatal(err)
		}
		if err := s.Delete("gone"); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := s.Get("gone"); !errors.Is(err, ErrNotFound) {
			t.Errorf("after delete: err = %v, want ErrNotFound", err)
		}
	})

	t.Run("invalid ids", func(t *testing.T) {
		s := newStore(t)
		bad := []string{"", "../evil", "a/b", `a\b`, ".hidden", "has space", "dot.json", strings.Repeat("a", 65)}
		for _, id := range bad {
			if err := s.Save(Character{ID: id, Name: "X"}); !errors.Is(err, ErrInvalidID) {
				t.Errorf("Save(%q): err = %v, want ErrInvalidID", id, err)
			}
			if _, err := s.Get(id); !errors.Is(err, ErrInvalidID) {
				t.Errorf("Get(%q): err = %v, want ErrInvalidID", id, err)
			}
			if err := s.Delete(id); !errors.Is(err, ErrInvalidID) {
				t.Errorf("Delete(%q): err = %v, want ErrInvalidID", id, err)
			}
		}
	})

	t.Run("list is sorted and summarised", func(t *testing.T) {
		s := newStore(t)
		for _, c := range []Character{
			sampleCharacter("c3", "Zed", cls("rogue", 3)),
			sampleCharacter("c1", "alice", cls("wizard", 5), cls("rogue", 2)),
			sampleCharacter("c2", "Bob", cls("fighter", 1)),
		} {
			if err := s.Save(c); err != nil {
				t.Fatal(err)
			}
		}
		list, err := s.List()
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, e := range list {
			names = append(names, e.Name)
		}
		if strings.Join(names, ",") != "alice,Bob,Zed" {
			t.Errorf("order = %v, want alice,Bob,Zed (case-insensitive by name)", names)
		}
		if list[0].Level != 7 || strings.Join(list[0].ClassIDs, ",") != "wizard,rogue" {
			t.Errorf("alice summary = %+v, want level 7 and classes wizard,rogue", list[0])
		}
		if list[0].ID != "c1" {
			t.Errorf("alice id = %q, want c1", list[0].ID)
		}
	})

	t.Run("get returns a copy", func(t *testing.T) {
		s := newStore(t)
		if err := s.Save(sampleCharacter("copy", "Original", cls("rogue", 1))); err != nil {
			t.Fatal(err)
		}
		got, _ := s.Get("copy")
		got.Name = "Changed"
		got.Selections[0].Allocated["str"] = 99

		again, _ := s.Get("copy")
		if again.Name != "Original" || again.Selections[0].Allocated["str"] != 2 {
			t.Errorf("changing a returned character must not affect the stored one: %+v", again)
		}
	})
}

func TestFileStoreSuite(t *testing.T) {
	runStoreSuite(t, func(t *testing.T) CharacterStore {
		s, err := NewFileStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		return s
	})
}

func TestMemoryStoreSuite(t *testing.T) {
	runStoreSuite(t, func(t *testing.T) CharacterStore { return NewMemoryStore() })
}

// ---------------------------------------------------------------------------
// FileStore specifics
// ---------------------------------------------------------------------------

func TestFileStoreCreatesItsDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "characters")
	if _, err := NewFileStore(dir); err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("directory %s was not created (err %v)", dir, err)
	}
}

func TestFileStoreLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewFileStore(dir)
	for i := 0; i < 3; i++ {
		if err := s.Save(sampleCharacter("one", "One", cls("rogue", 1))); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "one.json" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory contains %v, want only one.json", names)
	}
}

func TestFileStoreWritesReadableJSON(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewFileStore(dir)
	if err := s.Save(sampleCharacter("pretty", "Pretty", cls("rogue", 1))); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "pretty.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\n  \"name\": \"Pretty\"") {
		t.Errorf("file should be indented for hand editing:\n%s", data)
	}
}

func TestFileStoreToleratesOldFormatsAndUnknownFields(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewFileStore(dir)
	old := `{"id":"old","name":"Old","extra_field":1,
		"classes":[{"class_id":"rogue","level":2,"hit_die":8,"rolled_hp":[8,5]}]}`
	if err := os.WriteFile(filepath.Join(dir, "old.json"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := s.Get("old")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(c.Classes) != 1 || c.Classes[0].Level != 2 {
		t.Errorf("classes = %+v, want one level 2 class", c.Classes)
	}
}

func TestFileStoreFillsMissingIDFromFilename(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewFileStore(dir)
	if err := os.WriteFile(filepath.Join(dir, "noid.json"), []byte(`{"name":"No Id"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := s.Get("noid")
	if err != nil || c.ID != "noid" {
		t.Errorf("Get = %+v, %v; want id noid", c, err)
	}
}

func TestFileStoreListReportsUnreadableFiles(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewFileStore(dir)
	if err := s.Save(sampleCharacter("good", "Good", cls("rogue", 1))); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"bad.json":      `{not json`,
		"mismatch.json": `{"id":"other","name":"Mismatch"}`,
		".tmp-skip.json": `{}`, // temp files are ignored
		"notes.txt":     `ignored`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	list, err := s.List()
	if err != nil {
		t.Fatalf("List should not fail because of one bad file: %v", err)
	}
	byID := map[string]CharacterSummary{}
	for _, e := range list {
		byID[e.ID] = e
	}
	if len(byID) != 3 {
		t.Fatalf("list = %+v, want good, bad and mismatch only", list)
	}
	if byID["good"].Error != "" {
		t.Errorf("good character should have no error: %+v", byID["good"])
	}
	if byID["bad"].Error == "" || byID["mismatch"].Error == "" {
		t.Errorf("bad and mismatch should report errors: %+v / %+v", byID["bad"], byID["mismatch"])
	}
	if _, err := s.Get("mismatch"); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("Get(mismatch) err = %v, want an id mismatch error", err)
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
