package character

// loader_test.go - tests for content loading and validation.
// Most use in-memory packs (testing/fstest), so they need no files on disk.
// The example-pack tests read content/example and skip themselves if that folder isn't there.
// Written without being able to run them: a failure may be a test mistake as much as a code bug.

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func memPack(name string, files map[string]string) PackSource {
	m := fstest.MapFS{}
	for p, c := range files {
		m[p] = &fstest.MapFile{Data: []byte(c)}
	}
	return PackSource{Name: name, FS: m}
}

func mustLoad(t *testing.T, opts LoadOptions, packs ...PackSource) *ContentLibrary {
	t.Helper()
	lib, err := LoadPacks(opts, packs...)
	if err != nil {
		t.Fatalf("LoadPacks: %v", err)
	}
	return lib
}

func hasIssue(lib *ContentLibrary, sev Severity, substr string) bool {
	for _, is := range lib.Issues() {
		if is.Severity == sev && strings.Contains(is.Message, substr) {
			return true
		}
	}
	return false
}

func dumpIssues(t *testing.T, lib *ContentLibrary) {
	t.Helper()
	for _, is := range lib.Issues() {
		t.Log(is)
	}
}

// ---------------------------------------------------------------------------
// Parsing
// ---------------------------------------------------------------------------

func TestLoaderAcceptsObjectsAndArrays(t *testing.T) {
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"features/one.json":  `{"id":"one","name":"One","origin":"feat"}`,
		"features/many.json": `[{"id":"two","name":"Two","origin":"feat"},{"id":"three","name":"Three","origin":"feat"}]`,
	}))
	for _, id := range []string{"one", "two", "three"} {
		if _, err := lib.Feature(id); err != nil {
			t.Errorf("feature %q not loaded: %v", id, err)
		}
	}
	if lib.HasErrors() {
		dumpIssues(t, lib)
		t.Error("unexpected errors")
	}
}

func TestLoaderRejectsUnknownFields(t *testing.T) {
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"features/bad.json": `{"id":"x","name":"X","origin":"feat","descripton":"typo"}`,
	}))
	found := false
	for _, is := range lib.Issues() {
		if is.Severity == SevError && strings.Contains(is.Message, "descripton") {
			found = true
			if is.File != "features/bad.json" {
				t.Errorf("issue file = %q, want features/bad.json", is.File)
			}
		}
	}
	if !found {
		t.Errorf("expected an error naming the unknown field, got %v", lib.Issues())
	}
	if _, err := lib.Feature("x"); err == nil {
		t.Error("a file with an unknown field should be skipped, not half-loaded")
	}
}

func TestLoaderReportsLineNumbers(t *testing.T) {
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"features/broken.json": "{\n  \"id\": \"x\",\n  \"name\": \n}",
	}))
	if !hasIssue(lib, SevError, "near line 3") && !hasIssue(lib, SevError, "near line 4") {
		t.Errorf("expected a syntax error with a line number near the end, got %v", lib.Issues())
	}
}

func TestLoaderEmptyAndTrailingContent(t *testing.T) {
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"features/empty.json":    "   \n",
		"features/trailing.json": `{"id":"t","name":"T","origin":"feat"} {"id":"u"}`,
	}))
	if !hasIssue(lib, SevError, "empty") {
		t.Error("expected an error for the empty file")
	}
	if !hasIssue(lib, SevError, "unexpected content") {
		t.Error("expected an error for content after the first value")
	}
}

func TestLoaderIgnoresUnknownFolders(t *testing.T) {
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"notes/readme.json": `{}`,
	}))
	if !hasIssue(lib, SevWarning, "ignored") {
		t.Errorf("expected an ignored-file warning, got %v", lib.Issues())
	}
}

func TestLibraryNotFoundErrors(t *testing.T) {
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{}))
	if _, err := lib.Feature("nope"); err == nil {
		t.Error("expected an error for a missing feature")
	}
	if _, err := lib.Class("nope"); err == nil {
		t.Error("expected an error for a missing class")
	}
	if _, err := lib.Spell("nope"); err == nil {
		t.Error("expected an error for a missing spell")
	}
}

// ---------------------------------------------------------------------------
// Layering
// ---------------------------------------------------------------------------

func TestLoaderDuplicateIDInOnePack(t *testing.T) {
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"features/a.json": `{"id":"dup","name":"A","origin":"feat"}`,
		"features/b.json": `{"id":"dup","name":"B","origin":"feat"}`,
	}))
	if !hasIssue(lib, SevError, "duplicate id") {
		t.Errorf("expected a duplicate id error, got %v", lib.Issues())
	}
	if f, _ := lib.Feature("dup"); f.Name != "A" {
		t.Errorf("first definition should win inside a pack, got %q", f.Name)
	}
}

func TestLoaderLaterPackOverrides(t *testing.T) {
	base := memPack("base", map[string]string{"features/f.json": `{"id":"f","name":"Base","origin":"feat"}`})
	home := memPack("home", map[string]string{"features/f.json": `{"id":"f","name":"Override","origin":"feat"}`})
	lib := mustLoad(t, LoadOptions{}, base, home)

	if f, _ := lib.Feature("f"); f.Name != "Override" {
		t.Errorf("feature name = %q, want Override", f.Name)
	}
	if !hasIssue(lib, SevWarning, "overrides") {
		t.Errorf("expected an override warning, got %v", lib.Issues())
	}
	if lib.HasErrors() {
		t.Error("an override is a warning, not an error")
	}
}

func TestLoaderPackJSONSetsName(t *testing.T) {
	lib := mustLoad(t, LoadOptions{}, memPack("folder_name", map[string]string{
		"pack.json":         `{"id":"nice_name","name":"Nice","version":"1"}`,
		"features/bad.json": `{"id":"x","bogus":1}`,
	}))
	for _, is := range lib.Issues() {
		if is.Pack != "nice_name" {
			t.Errorf("issue pack = %q, want nice_name", is.Pack)
		}
	}
}

func TestSkillOverrideKeepsPosition(t *testing.T) {
	one := memPack("one", map[string]string{"skills.json": `[
		{"id":"a","name":"A","default_ability":"str"},
		{"id":"b","name":"B","default_ability":"dex"}]`})
	two := memPack("two", map[string]string{"skills.json": `[{"id":"b","name":"B2","default_ability":"wis"}]`})
	lib := mustLoad(t, LoadOptions{}, one, two)

	sk := lib.Skills()
	if len(sk) != 2 || sk[0].ID != "a" || sk[1].ID != "b" || sk[1].Name != "B2" || sk[1].DefaultAbility != Wis {
		t.Errorf("skills = %+v, want a then an overridden b", sk)
	}
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

func TestValidationDanglingClassFeature(t *testing.T) {
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"classes/c.json": `{"id":"c","name":"C","hit_die":8,"levels":[["missing_feature"]]}`,
	}))
	if !hasIssue(lib, SevError, "unknown feature") {
		t.Errorf("expected an unknown feature error, got %v", lib.Issues())
	}
}

func TestValidationBadModifiers(t *testing.T) {
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"features/f.json": `{"id":"f","name":"F","origin":"feat","modifiers":[
			{"target":"skil.stealth","kind":"add","value":1},
			{"target":"ac","kind":"plus","value":1},
			{"target":"damage.melee","kind":"add","dice":"d6"}]}`,
	}))
	for _, want := range []string{"unknown target", "unknown kind", "must look like"} {
		if !hasIssue(lib, SevError, want) {
			t.Errorf("expected an error containing %q, got %v", want, lib.Issues())
		}
	}
}

func TestValidationChoiceProblems(t *testing.T) {
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"features/f.json": `{"id":"f","name":"F","origin":"feat","choices":[
			{"id":"c","prompt":"p","kind":"pick","count":0,
			 "options":[{"id":"a","name":"A"}],"filter":{"type":"skill"}}]}`,
	}))
	for _, want := range []string{"count of at least 1", "exactly one of"} {
		if !hasIssue(lib, SevError, want) {
			t.Errorf("expected an error containing %q, got %v", want, lib.Issues())
		}
	}
}

func TestValidationSkillReferences(t *testing.T) {
	feature := `{"id":"f","name":"F","origin":"feat",
		"grants":{"skills":{"stealth":"trained"}},
		"modifiers":[{"target":"skill.stealth","kind":"add","value":1}]}`

	without := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{"features/f.json": feature}))
	if !hasIssue(without, SevError, "unknown skill") || !hasIssue(without, SevError, "unknown target") {
		t.Errorf("expected unknown skill and target errors without a skills file, got %v", without.Issues())
	}

	with := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"skills.json":     `[{"id":"stealth","name":"Stealth","default_ability":"dex"}]`,
		"features/f.json": feature,
	}))
	if with.HasErrors() {
		dumpIssues(t, with)
		t.Error("a defined skill should satisfy both references")
	}
}

func TestValidationSpellContent(t *testing.T) {
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"spells/s.json": `{"id":"s","name":"S","level":12,"save":"luck","effects":[
			{"kind":"burn","dice":"3d4+3"}]}`,
		"spell_lists/l.json": `{"id":"l","name":"L","spell_ids":["ghost"]}`,
	}))
	for _, want := range []string{"between 0", "is not an ability", "must be damage or healing", "must look like", "unknown spell"} {
		if !hasIssue(lib, SevError, want) {
			t.Errorf("expected an error containing %q, got %v", want, lib.Issues())
		}
	}
}

func TestValidationClassSpellcasting(t *testing.T) {
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"classes/c.json": `{"id":"c","name":"C","hit_die":6,"levels":[],
			"spellcasting":{"ability":"int","divisor":1,"prep":"sometimes","spell_list_id":"nothing"}}`,
	}))
	for _, want := range []string{"prep", "unknown spell list"} {
		if !hasIssue(lib, SevError, want) {
			t.Errorf("expected an error containing %q, got %v", want, lib.Issues())
		}
	}
}

func TestStrictModeReturnsAnError(t *testing.T) {
	bad := memPack("p", map[string]string{
		"features/f.json": `{"id":"f","name":"F","origin":"feat","modifiers":[{"target":"ac","kind":"plus"}]}`,
	})

	lib, err := LoadPacks(LoadOptions{Strict: true}, bad)
	if err == nil {
		t.Error("strict mode should return an error for content errors")
	}
	if lib == nil || !lib.HasErrors() {
		t.Error("strict mode should still hand back the library so issues can be printed")
	}

	lib, err = LoadPacks(LoadOptions{}, bad)
	if err != nil || !lib.HasErrors() {
		t.Errorf("permissive mode should load and report: err=%v, hasErrors=%v", err, lib != nil && lib.HasErrors())
	}
}

// ---------------------------------------------------------------------------
// The example pack, end to end
// ---------------------------------------------------------------------------

func loadExample(t *testing.T) *ContentLibrary {
	t.Helper()
	if _, err := os.Stat("content/example"); err != nil {
		t.Skip("content/example not found: copy the content folder next to the Go files to run this test")
	}
	lib, err := LoadDirs(LoadOptions{Strict: true}, "content/example")
	if err != nil {
		if lib != nil {
			dumpIssues(t, lib)
		}
		t.Fatalf("example pack failed to load: %v", err)
	}
	return lib
}

// noContentWarnings fails on sheet warnings that would mean the content and the engine disagree.
func noContentWarnings(t *testing.T, sh Sheet) {
	t.Helper()
	for _, w := range sh.Warnings {
		switch w.Code {
		case WarnUnknownID, WarnUnknownTarget, WarnNotInPool, WarnBadDice,
			WarnUnknownTerm, WarnSpellTooHigh, WarnMissingLevelData:
			t.Errorf("unexpected warning %s: %s", w.Code, w.Message)
		}
	}
}

func TestExamplePackLoadsClean(t *testing.T) {
	lib := loadExample(t)
	c := lib.Counts()
	if c["skills"] != 18 || c["classes"] != 2 || c["spell_lists"] != 2 {
		t.Errorf("counts = %v", c)
	}
	for _, is := range lib.Issues() {
		t.Log(is) // warnings are fine; they are shown with -v
	}
}

func TestExampleRogueEndToEnd(t *testing.T) {
	lib := loadExample(t)
	c := Character{
		ID: "r1", Name: "Test Rogue",
		Abilities: map[Ability]AbilityScore{
			Str: {Score: 10}, Dex: {Score: 16}, Con: {Score: 14}, Int: {Score: 12}, Wis: {Score: 10}, Cha: {Score: 10},
		},
		Classes: []ClassLevel{{ClassID: "rogue", SubclassID: "thief", Level: 5, RolledHP: []int{8, 5, 5, 5, 5}}},
		Selections: []Selection{
			{FeatureID: "class:rogue:start", ChoiceID: "skills", Picked: []string{"stealth", "perception", "acrobatics", "sleight_of_hand"}},
			{FeatureID: "rogue_expertise_1", ChoiceID: "expertise", Picked: []string{"stealth", "sleight_of_hand"}},
		},
		Inventory: []InventoryItem{
			{ItemID: "leather_armor", Quantity: 1, Equipped: true},
			{ItemID: "dagger", Quantity: 1, Equipped: true},
		},
	}
	sh := mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)

	if sh.ProficiencyBonus != 3 {
		t.Errorf("PB = %d, want 3", sh.ProficiencyBonus)
	}
	if got := sh.Abilities[Dex].SaveBonus; got != 6 { // +3 mod, +3 PB
		t.Errorf("dex save = %d, want 6", got)
	}
	if got := sh.Abilities[Int].SaveBonus; got != 4 { // +1 mod, +3 PB
		t.Errorf("int save = %d, want 4", got)
	}
	if got := sh.Abilities[Wis].SaveProf; got {
		t.Error("wis save should not be proficient")
	}
	if got := sh.Skills["stealth"].Bonus; got != 9 { // +3 mod, expertise 2 x 3
		t.Errorf("stealth = %d, want 9", got)
	}
	if got := sh.Skills["perception"].Passive; got != 13 { // 10 + 0 + 3
		t.Errorf("passive perception = %d, want 13", got)
	}
	if sh.AC != 14 || sh.ACDetail.FormulaID != "leather_armor" { // 11 + Dex 3
		t.Errorf("AC = %d via %q, want 14 via leather_armor", sh.AC, sh.ACDetail.FormulaID)
	}
	if sh.MaxHP != 38 { // 28 rolled + 5 levels x Con +2
		t.Errorf("max HP = %d, want 38", sh.MaxHP)
	}

	a, ok := findAttack(sh, "Dagger")
	if !ok {
		t.Fatalf("no dagger attack in %+v", sh.Attacks)
	}
	if a.Ability != Dex || a.ToHit != 6 || a.DamageBonus != 3 {
		t.Errorf("dagger = %s, to hit %d, damage %d; want dex, 6, 3", a.Ability, a.ToHit, a.DamageBonus)
	}
	if len(a.DamageRiders) != 1 || a.DamageRiders[0].Dice != "3d6" {
		t.Errorf("sneak attack riders = %+v, want one 3d6 at level 5", a.DamageRiders)
	}

	if _, ok := findPending(sh, "rogue_asi_4", "feat"); !ok {
		t.Error("the level 4 feat (or ability score improvement) should be pending")
	}
	if _, ok := findPending(sh, "class:rogue:start", "skills"); ok {
		t.Error("starting skills were all chosen")
	}
	if _, ok := findPending(sh, "class:rogue", "subclass"); ok {
		t.Error("the subclass was chosen")
	}
}

func TestExampleWizardEndToEnd(t *testing.T) {
	lib := loadExample(t)
	c := Character{
		ID: "w1", Name: "Test Wizard",
		Abilities: map[Ability]AbilityScore{Int: {Score: 16}},
		Classes:   []ClassLevel{{ClassID: "wizard", Level: 5, RolledHP: []int{6, 4, 4, 4, 4}}},
		Selections: []Selection{
			{FeatureID: "class:wizard:start", ChoiceID: "skills", Picked: []string{"arcana", "history"}},
		},
		Spellbooks: map[string]SpellState{"class:wizard": {
			Cantrips: []string{"fire_bolt", "mage_hand"},
			Book:     []string{"fireball", "magic_missile", "shield"},
			Prepared: []string{"fireball", "magic_missile", "shield"},
		}},
	}
	sh := mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)

	got := slotMaxes(sh)
	if got[1] != 4 || got[2] != 3 || got[3] != 2 || len(got) != 3 {
		t.Errorf("slots = %v, want 4/3/2", got)
	}

	src, ok := findSource(sh, "class:wizard")
	if !ok {
		t.Fatalf("no wizard source in %+v", sh.Spellcasting)
	}
	if src.SaveDC != 14 || src.CantripsAllowed != 4 || src.SpellsAllowed != 8 { // 8+3+3; table; 3 + level 5
		t.Errorf("DC %d, cantrips allowed %d, spells allowed %d; want 14, 4, 8", src.SaveDC, src.CantripsAllowed, src.SpellsAllowed)
	}
	bolt, ok := findSpell(src.Cantrips, "fire_bolt")
	if !ok || len(bolt.Effects) != 1 || bolt.Effects[0].Dice != "2d10" {
		t.Errorf("fire bolt at level 5 = %+v, want 2d10", bolt)
	}
	ball, ok := findSpell(src.Spells, "fireball")
	if !ok || ball.SaveDC != 14 || len(ball.Effects) != 1 || ball.Effects[0].Dice != "8d6" {
		t.Errorf("fireball = %+v, want DC 14 and 8d6", ball)
	}

	if p, ok := findPending(sh, "spells:class:wizard", "cantrips"); !ok || p.Remaining != 2 {
		t.Errorf("expected 2 cantrips pending, got %+v", sh.PendingChoices)
	}
	if p, ok := findPending(sh, "spells:class:wizard", "prepared"); !ok || p.Remaining != 5 {
		t.Errorf("expected 5 prepared spells pending, got %+v", sh.PendingChoices)
	}

	found := false
	for _, r := range sh.Resources {
		if r.FeatureID == "arcane_recovery" && r.Max == 1 {
			found = true
		}
	}
	if !found {
		t.Errorf("arcane recovery resource missing: %+v", sh.Resources)
	}
}

func TestHomebrewPackOverridesExample(t *testing.T) {
	if _, err := os.Stat("content/example"); err != nil {
		t.Skip("content/example not found")
	}
	example := PackSource{Name: "example", FS: os.DirFS("content/example")}
	homebrew := memPack("homebrew", map[string]string{
		"items/dagger.json": `{"id":"dagger","name":"Dagger","kind":"weapon","weapon":{"category":"simple",
			"attack":{"damage_dice":"1d6","damage_type":"piercing","range_normal":5,"properties":["finesse","light","thrown"]}}}`,
	})
	lib := mustLoad(t, LoadOptions{}, example, homebrew)

	it, err := lib.Item("dagger")
	if err != nil || it.Weapon == nil || it.Weapon.Attack.DamageDice != "1d6" {
		t.Errorf("dagger = %+v (err %v), want the homebrew 1d6 version", it, err)
	}
	if !hasIssue(lib, SevWarning, "overrides") {
		t.Error("expected an override warning")
	}
	if lib.HasErrors() {
		dumpIssues(t, lib)
		t.Error("overriding should not produce errors")
	}
}
