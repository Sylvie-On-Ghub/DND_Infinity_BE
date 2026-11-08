package character

// background_test.go - tests for backgrounds and races: features tagged "background" / "race", chosen by
// setting background_id / race_id. Written without being able to run them: a failure may be a test
// mistake as much as a code bug.

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

// realPending is the pending list without "choose a race" and "choose a background", for tests whose
// character deliberately has neither while the loaded content offers them.
func realPending(sh Sheet) []PendingChoice {
	out := []PendingChoice{}
	for _, p := range sh.PendingChoices {
		if p.FeatureID == "character" && (p.ChoiceID == "background" || p.ChoiceID == "race") {
			continue
		}
		out = append(out, p)
	}
	return out
}

func kindPack(t *testing.T) *ContentLibrary {
	t.Helper()
	return mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"features/f.json": `[
			{"id":"bg","name":"Drifter","origin":"background","tags":["background"]},
			{"id":"trait","name":"Drifter's Luck","origin":"background"},
			{"id":"kin","name":"Kin","origin":"race","tags":["race"]},
			{"id":"cls","name":"Class Thing","origin":"class"}]`,
	}))
}

// ---------------------------------------------------------------------------
// Wrong kind
// ---------------------------------------------------------------------------

func TestWrongKindWarnings(t *testing.T) {
	lib := kindPack(t)
	cases := []struct {
		name           string
		background     string
		race           string
		wantWrong      string // the id expected in a wrong_kind warning, or "" for none
		wantUnknownFor string
	}{
		{"a real background", "bg", "", "", ""},
		{"a real race", "", "kin", "", ""},
		{"a background trait is not a background", "trait", "", "trait", ""},
		{"a class feature is not a background", "cls", "", "cls", ""},
		{"a background is not a race", "", "bg", "bg", ""},
		{"an unknown id is reported as unknown, not as the wrong kind", "nope", "", "", "nope"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newChar(1, nil)
			c.BackgroundID, c.RaceID = tc.background, tc.race
			sh := mustCompute(t, c, lib, Ruleset{})

			if tc.wantWrong == "" {
				if hasWarning(sh, WarnWrongKind) {
					t.Errorf("unexpected wrong_kind warning: %+v", sh.Warnings)
				}
			} else if !hasWarningValue(sh, WarnWrongKind, tc.wantWrong) {
				t.Errorf("expected wrong_kind for %q, got %+v", tc.wantWrong, sh.Warnings)
			}
			if tc.wantUnknownFor != "" && !hasWarningValue(sh, WarnUnknownID, tc.wantUnknownFor) {
				t.Errorf("expected unknown_id for %q, got %+v", tc.wantUnknownFor, sh.Warnings)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The pending "choose a background" item
// ---------------------------------------------------------------------------

func TestPendingBackground(t *testing.T) {
	lib := kindPack(t)

	sh := mustCompute(t, newChar(1, nil), lib, Ruleset{})
	if p, ok := findPending(sh, "character", "background"); !ok || p.Remaining != 1 {
		t.Errorf("a character with no background should be asked to choose one: %+v", sh.PendingChoices)
	}

	c := newChar(1, nil)
	c.BackgroundID = "bg"
	if _, ok := findPending(mustCompute(t, c, lib, Ruleset{}), "character", "background"); ok {
		t.Error("choosing a background should clear the pending item")
	}

	// Content with no backgrounds to offer never nags.
	none := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"features/t.json": `{"id":"trait","name":"Just a trait","origin":"background"}`,
	}))
	if _, ok := findPending(mustCompute(t, newChar(1, nil), none, Ruleset{}), "character", "background"); ok {
		t.Error("no pending background when the content has none to choose from")
	}
	if _, ok := findPending(mustCompute(t, newChar(1, nil), fakeLib{}, Ruleset{}), "character", "background"); ok {
		t.Error("no pending background when the library can't list its features")
	}
}

// ---------------------------------------------------------------------------
// Listing and the loader
// ---------------------------------------------------------------------------

func TestListBackgroundsAndRaces(t *testing.T) {
	lib := kindPack(t)

	bgs, err := lib.List("backgrounds")
	if err != nil || len(bgs) != 1 || bgs[0].ID != "bg" {
		t.Errorf("backgrounds = %+v (err %v), want only the tagged one, not its trait", bgs, err)
	}
	races, err := lib.List("races")
	if err != nil || len(races) != 1 || races[0].ID != "kin" {
		t.Errorf("races = %+v (err %v), want kin", races, err)
	}

	if v, err := lib.Lookup("backgrounds", "bg"); err != nil || v.(Feature).Name != "Drifter" {
		t.Errorf("lookup bg = %v (err %v)", v, err)
	}
	for _, miss := range [][2]string{{"backgrounds", "trait"}, {"backgrounds", "kin"}, {"races", "bg"}, {"races", "nope"}} {
		if _, err := lib.Lookup(miss[0], miss[1]); !isNotFoundErr(err) {
			t.Errorf("lookup %s/%s: err = %v, want not found", miss[0], miss[1], err)
		}
	}
}

func TestLoaderWarnsWhenTagAndOriginDisagree(t *testing.T) {
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"features/f.json": `[
			{"id":"a","name":"A","origin":"class","tags":["background"]},
			{"id":"b","name":"B","origin":"feat","tags":["race"]}]`,
	}))
	if !hasIssue(lib, SevWarning, `is tagged "background"`) || !hasIssue(lib, SevWarning, `is tagged "race"`) {
		t.Errorf("expected two tag/origin warnings, got %v", lib.Issues())
	}
}

// ---------------------------------------------------------------------------
// The example pack: backgrounds with feats and ability score increases
// ---------------------------------------------------------------------------

func TestExampleScholarBackground(t *testing.T) {
	lib := loadElfLib(t)
	c := Character{
		ID: "s", Name: "Newcomer", BackgroundID: "scholar", RaceID: "dwarf",
		Abilities: map[Ability]AbilityScore{Con: {Score: 14}, Int: {Score: 12}, Wis: {Score: 10}},
	}
	sh := mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)

	if _, ok := findPending(sh, "character", "background"); ok {
		t.Error("the background is chosen, so it shouldn't be pending")
	}
	// Its ability increase and the feat it grants both want decisions.
	if p, ok := findPending(sh, "scholar", "scores"); !ok || p.Remaining != 3 {
		t.Errorf("expected 3 ability points pending on the scholar, got %+v", sh.PendingChoices)
	}
	if p, ok := findPending(sh, "skilled", "skills"); !ok || p.Remaining != 3 {
		t.Errorf("the Skilled feat the scholar grants should want 3 skills, got %+v", sh.PendingChoices)
	}

	// Fixed grants apply straight away.
	if sh.Skills["arcana"].Level != ProfTrained || sh.Skills["history"].Level != ProfTrained {
		t.Errorf("scholar skills: arcana %q, history %q; want both trained", sh.Skills["arcana"].Level, sh.Skills["history"].Level)
	}
	if _, ok := sh.Tools["calligraphers_supplies"]; !ok {
		t.Errorf("scholar tool proficiency missing: %+v", sh.Tools)
	}

	// The race: speed and darkvision.
	if sh.Speeds[SpeedWalk] != 30 {
		t.Errorf("walk speed = %d, want 30", sh.Speeds[SpeedWalk])
	}
	if darkvision(sh) != 120 {
		t.Errorf("a dwarf should have 120 foot darkvision: %+v", sh.Senses)
	}

	// Make the choices.
	c.Selections = []Selection{
		{FeatureID: "scholar", ChoiceID: "scores", Allocated: map[string]int{"int": 2, "wis": 1}},
		{FeatureID: "skilled", ChoiceID: "skills", Picked: []string{"stealth", "perception", "insight"}},
	}
	sh = mustCompute(t, c, lib, Ruleset{})
	if sh.Abilities[Int].Score != 14 || sh.Abilities[Wis].Score != 11 {
		t.Errorf("Int %d, Wis %d; want 14 and 11", sh.Abilities[Int].Score, sh.Abilities[Wis].Score)
	}
	if sh.Skills["stealth"].Level != ProfTrained {
		t.Errorf("the Skilled picks should be applied: stealth is %q", sh.Skills["stealth"].Level)
	}
	if left := realPending(sh); len(left) != 0 {
		t.Errorf("everything is decided, but %+v is pending", left)
	}
}

func TestExampleBackgroundFeats(t *testing.T) {
	lib := loadExample(t)

	vet := mustCompute(t, Character{ID: "v", BackgroundID: "veteran"}, lib, Ruleset{})
	if vet.Initiative != 5 { // Dex 0, plus the Alert feat the veteran grants
		t.Errorf("veteran initiative = %d, want 5", vet.Initiative)
	}

	wan := mustCompute(t, Character{ID: "w", BackgroundID: "wanderer"}, lib, Ruleset{})
	if wan.Skills["perception"].Passive != 15 { // 10 + 0, plus the Watchful feat
		t.Errorf("wanderer passive perception = %d, want 15", wan.Skills["perception"].Passive)
	}
}

// Skilled is repeatable: the background's copy and one taken later at a feat level are separate.
func TestBackgroundFeatAndALaterPickOfTheSameFeatStaySeparate(t *testing.T) {
	lib := loadExample(t)
	c := Character{
		ID: "r", Name: "Twice Skilled", BackgroundID: "scholar",
		Classes: []ClassLevel{cls("rogue", 4)},
		Selections: []Selection{
			featPick("rogue_asi_4", "skilled"),
			{FeatureID: "skilled", ChoiceID: "skills", Picked: []string{"stealth", "perception", "insight"}},
		},
	}
	sh := mustCompute(t, c, lib, Ruleset{})

	if _, ok := findPending(sh, "skilled", "skills"); ok {
		t.Error("the background's Skilled picks are made, so they shouldn't be pending")
	}
	if p, ok := findPending(sh, "skilled@rogue_asi_4:feat", "skills"); !ok || p.Remaining != 3 {
		t.Errorf("the second Skilled should have its own 3 picks pending, got %+v", sh.PendingChoices)
	}

	c.Selections = append(c.Selections, Selection{
		FeatureID: "skilled@rogue_asi_4:feat", ChoiceID: "skills", Picked: []string{"acrobatics", "athletics", "deception"},
	})
	sh = mustCompute(t, c, lib, Ruleset{})
	for _, id := range []string{"stealth", "perception", "insight", "acrobatics", "athletics", "deception"} {
		if sh.Skills[id].Level != ProfTrained {
			t.Errorf("%s should be trained from one of the two Skilled picks", id)
		}
	}
}

// ---------------------------------------------------------------------------
// The API
// ---------------------------------------------------------------------------

func TestAPIBackgrounds(t *testing.T) {
	srv, store := newTestServer(t, func(c *Config) { c.Library = loadElfLib(t) })
	saveFixture(t, store, Character{ID: "new", Name: "Newcomer"})

	rec := call(t, srv, http.MethodGet, "/api/content/backgrounds", nil)
	wantStatus(t, rec, http.StatusOK)
	var bgs []ContentSummary
	into(t, rec, &bgs)
	if len(bgs) != 8 || bgs[0].ID != "custom" || bgs[1].ID != "entertainer" || bgs[2].ID != "guard" || bgs[3].ID != "hermit" || bgs[4].ID != "scholar" || bgs[5].ID != "veteran" || bgs[6].ID != "wanderer" || bgs[7].ID != "wayfarer" {
		t.Errorf("backgrounds = %+v, want custom, entertainer, guard, hermit, scholar, veteran, wanderer, wayfarer", bgs)
	}

	rec = call(t, srv, http.MethodGet, "/api/content/races", nil)
	var races []ContentSummary
	into(t, rec, &races)
	ids := []string{}
	for _, r := range races {
		ids = append(ids, r.ID)
	}
	if !slices.Equal(ids, []string{"aasimar", "dragonborn", "dwarf", "elf", "gnome", "goliath", "halfling", "human", "orc", "tiefling"}) {
		t.Errorf("races = %v, want dragonborn, dwarf, elf, gnome, human and tiefling", ids)
	}

	rec = call(t, srv, http.MethodGet, "/api/content/backgrounds/scholar", nil)
	wantStatus(t, rec, http.StatusOK)
	var scholar Feature
	into(t, rec, &scholar)
	if scholar.Name != "Scholar" || len(scholar.Choices) != 1 {
		t.Errorf("scholar = %+v", scholar)
	}
	wantError(t, call(t, srv, http.MethodGet, "/api/content/backgrounds/alert", nil), http.StatusNotFound, "not_found")

	sheetOf := func() Sheet {
		rec := call(t, srv, http.MethodGet, "/api/characters/new/sheet", nil)
		wantStatus(t, rec, http.StatusOK)
		var sh Sheet
		into(t, rec, &sh)
		return sh
	}

	if _, ok := findPending(sheetOf(), "character", "background"); !ok {
		t.Error("a new character should be asked to choose a background")
	}

	wantStatus(t, call(t, srv, http.MethodPatch, "/api/characters/new", withVersion(t, srv, "new", `{"background_id":"scholar"}`)), http.StatusOK)
	sh := sheetOf()
	if _, ok := findPending(sh, "character", "background"); ok {
		t.Error("the background is chosen")
	}
	if _, ok := findPending(sh, "scholar", "scores"); !ok {
		t.Errorf("the scholar's ability increase should be pending: %+v", sh.PendingChoices)
	}

	// A feat is not a background.
	wantStatus(t, call(t, srv, http.MethodPatch, "/api/characters/new", withVersion(t, srv, "new", `{"background_id":"alert"}`)), http.StatusOK)
	if !hasWarning(sheetOf(), WarnWrongKind) {
		t.Error("a background_id that names a feat should produce a wrong_kind warning")
	}
}

// ---------------------------------------------------------------------------
// The pending "choose a race" item
// ---------------------------------------------------------------------------

func TestPendingRace(t *testing.T) {
	lib := kindPack(t)

	sh := mustCompute(t, newChar(1, nil), lib, Ruleset{})
	if p, ok := findPending(sh, "character", "race"); !ok || p.Remaining != 1 {
		t.Errorf("a character with no race should be asked to choose one: %+v", sh.PendingChoices)
	}

	c := newChar(1, nil)
	c.RaceID = "kin"
	sh = mustCompute(t, c, lib, Ruleset{})
	if _, ok := findPending(sh, "character", "race"); ok {
		t.Error("choosing a race should clear the pending item")
	}
	if _, ok := findPending(sh, "character", "background"); !ok {
		t.Error("choosing a race must not clear the background item")
	}

	// A wrong id still counts as chosen: the wrong_kind warning is what tells you, not a second nag.
	c.RaceID = "trait"
	if _, ok := findPending(mustCompute(t, c, lib, Ruleset{}), "character", "race"); ok {
		t.Error("a race_id that is set (even wrongly) shouldn't also be pending")
	}

	// Content with no races to offer never nags.
	none := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"features/t.json": `{"id":"trait","name":"Just a trait","origin":"race"}`,
	}))
	if _, ok := findPending(mustCompute(t, newChar(1, nil), none, Ruleset{}), "character", "race"); ok {
		t.Error("no pending race when the content has none to choose from")
	}
	if _, ok := findPending(mustCompute(t, newChar(1, nil), fakeLib{}, Ruleset{}), "character", "race"); ok {
		t.Error("no pending race when the library can't list its features")
	}
}

func TestAPIRaces(t *testing.T) {
	srv, store := newTestServer(t, func(c *Config) { c.Library = loadElfLib(t) })
	saveFixture(t, store, Character{ID: "new", Name: "Newcomer"})

	sheetOf := func() Sheet {
		rec := call(t, srv, http.MethodGet, "/api/characters/new/sheet", nil)
		wantStatus(t, rec, http.StatusOK)
		var sh Sheet
		into(t, rec, &sh)
		return sh
	}

	if _, ok := findPending(sheetOf(), "character", "race"); !ok {
		t.Error("a new character should be asked to choose a race")
	}
	wantStatus(t, call(t, srv, http.MethodPatch, "/api/characters/new", withVersion(t, srv, "new", `{"race_id":"dwarf"}`)), http.StatusOK)
	sh := sheetOf()
	if _, ok := findPending(sh, "character", "race"); ok {
		t.Error("the race is chosen")
	}
	if sh.Speeds[SpeedWalk] != 30 {
		t.Errorf("walk speed = %d, want 30 from the race", sh.Speeds[SpeedWalk])
	}
}

func TestExampleWayfarer(t *testing.T) {
	lib := loadExample(t)
	c := Character{ID: "w", BackgroundID: "wayfarer", Selections: []Selection{
		{FeatureID: "wayfarer", ChoiceID: "scores", Allocated: map[string]int{"dex": 2, "cha": 1}},
	}}
	sh := mustCompute(t, c, lib, Ruleset{})

	if sh.Abilities[Dex].Score != 12 || sh.Abilities[Cha].Score != 11 {
		t.Errorf("Dex %d, Cha %d; want 12 and 11", sh.Abilities[Dex].Score, sh.Abilities[Cha].Score)
	}
	if sh.Skills["insight"].Level != ProfTrained || sh.Skills["stealth"].Level != ProfTrained {
		t.Errorf("insight %q, stealth %q; want both trained", sh.Skills["insight"].Level, sh.Skills["stealth"].Level)
	}
	if sh.Tools["thieves_tools"].Level != ProfTrained {
		t.Errorf("thieves' tools proficiency missing: %+v", sh.Tools)
	}

	// Lucky: Luck Points equal to the proficiency bonus, so 2 at level 1 and 3 at level 5.
	luck := func(sh Sheet) int {
		for _, r := range sh.Resources {
			if r.FeatureID == "lucky" {
				return r.Max
			}
		}
		return -1
	}
	if n := luck(sh); n != 2 {
		t.Errorf("luck points = %d, want 2", n)
	}
	c.Classes = []ClassLevel{{ClassID: "rogue", Level: 5}}
	if n := luck(mustCompute(t, c, lib, Ruleset{})); n != 3 {
		t.Errorf("luck points at level 5 = %d, want 3", n)
	}
}

func TestExampleToughFeat(t *testing.T) {
	lib := loadExample(t)
	for _, lvl := range []int{1, 4, 20} {
		c := Character{ID: "t", Classes: []ClassLevel{{ClassID: "rogue", Level: lvl}}}
		without := mustCompute(t, c, lib, Ruleset{}).MaxHP
		c.FeatureIDs = []string{"tough"}
		with := mustCompute(t, c, lib, Ruleset{}).MaxHP
		if with-without != 2*lvl {
			t.Errorf("level %d: Tough adds %d max HP, want %d", lvl, with-without, 2*lvl)
		}
	}
}

func TestExampleMundaneEquipment(t *testing.T) {
	lib := loadExample(t)
	c := Character{ID: "e", Abilities: map[Ability]AbilityScore{Dex: {Score: 16}},
		Classes: []ClassLevel{{ClassID: "rogue", Level: 1}},
		Inventory: []InventoryItem{
			{ID: "a", ItemID: "plate_armor", Quantity: 1, Equipped: true},
			{ID: "b", ItemID: "glaive", Quantity: 1, Equipped: true},
			{ID: "c", ItemID: "blowgun", Quantity: 1, Equipped: true},
			{ID: "d", ItemID: "explorers_pack", Quantity: 1},
			{ID: "e", ItemID: "pouch", Quantity: 1},
			{ID: "f", ItemID: "lute", Quantity: 1},
		}}
	sh := mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)

	if sh.AC != 18 { // plate ignores Dex
		t.Errorf("AC in plate = %d, want 18", sh.AC)
	}
	if !sh.Skills["stealth"].Disadvantage {
		t.Error("plate armour should give disadvantage on Stealth")
	}
	byItem := map[string]AttackView{}
	for _, a := range sh.Attacks {
		byItem[a.ItemID] = a
	}
	if g := byItem["glaive"]; g.DamageDice != "1d10" || g.RangeNormal != 10 {
		t.Errorf("glaive = %+v, want 1d10 with 10 ft reach", g)
	}
	if b := byItem["blowgun"]; b.RangeNormal != 25 || b.RangeLong != 100 {
		t.Errorf("blowgun = %+v, want range 25/100", b)
	}
	for _, id := range []string{"explorers_pack", "pouch", "lute"} {
		found := false
		for _, v := range sh.Inventory {
			if v.ItemID == id {
				found = true
			}
		}
		if !found {
			t.Errorf("%s missing from the inventory", id)
		}
	}
}

func TestGuardBackground(t *testing.T) {
	lib := loadExample(t)
	c := Character{ID: "g", BackgroundID: "guard", Classes: []ClassLevel{{ClassID: "rogue", Level: 1}},
		Abilities: map[Ability]AbilityScore{Str: {Score: 10}, Int: {Score: 10}, Wis: {Score: 10}, Dex: {Score: 10}}}

	sh := mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	for _, id := range []string{"scores", "gaming_set"} {
		if _, ok := findPending(sh, "guard", id); !ok {
			t.Errorf("%s should be pending: %+v", id, sh.PendingChoices)
		}
	}
	opts, err := ChoiceOptions(c, lib, Ruleset{}, "guard", "gaming_set")
	if err != nil || len(opts) != 4 || opts[0].Name != "Dice" {
		t.Errorf("gaming set options = %+v (err %v)", opts, err)
	}

	c.Selections = []Selection{
		{FeatureID: "guard", ChoiceID: "scores", Allocated: map[string]int{"str": 2, "wis": 1}},
		{FeatureID: "guard", ChoiceID: "gaming_set", Picked: []string{"dice"}},
	}
	sh = mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	if _, ok := findPending(sh, "guard", "gaming_set"); ok {
		t.Error("the gaming set has been chosen")
	}
	if sh.Abilities[Str].Score != 12 || sh.Abilities[Wis].Score != 11 || sh.Abilities[Int].Score != 10 {
		t.Errorf("Str %d, Wis %d, Int %d", sh.Abilities[Str].Score, sh.Abilities[Wis].Score, sh.Abilities[Int].Score)
	}
	for _, skill := range []string{"athletics", "perception"} {
		if sh.Skills[skill].Level != ProfTrained {
			t.Errorf("%s = %+v", skill, sh.Skills[skill])
		}
	}
	if sh.Tools["gaming_set_dice"].Level != ProfTrained {
		t.Errorf("tools = %+v", sh.Tools)
	}
	if sh.Initiative != 5 { // Dex 10, plus 5 from Alert
		t.Errorf("initiative = %d", sh.Initiative)
	}
	found := false
	for _, f := range sh.Features {
		if f.Name == "Alert" {
			found = true
		}
	}
	if !found {
		t.Error("the Alert feat is missing")
	}
}

func TestCustomBackground(t *testing.T) {
	lib := loadExample(t)
	c := Character{ID: "c", BackgroundID: "custom", Classes: []ClassLevel{{ClassID: "rogue", Level: 1}},
		Abilities: map[Ability]AbilityScore{Str: {Score: 10}, Dex: {Score: 10}, Con: {Score: 10}, Int: {Score: 10}, Wis: {Score: 10}, Cha: {Score: 10}}}

	sh := mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	for id, remaining := range map[string]int{"scores": 3, "skills": 2, "feat": 1} {
		if p, ok := findPending(sh, "custom", id); !ok || p.Remaining != remaining {
			t.Errorf("%s: pending = %+v, want %d remaining", id, sh.PendingChoices, remaining)
		}
	}

	// Any skill, and every origin feat but nothing else.
	skills, err := ChoiceOptions(c, lib, Ruleset{}, "custom", "skills")
	if err != nil || len(skills) != len(lib.Skills()) {
		t.Errorf("skill options = %+v (err %v)", skills, err)
	}
	feats, err := ChoiceOptions(c, lib, Ruleset{}, "custom", "feat")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, o := range feats {
		names[o.OptionID] = true
	}
	if !names["alert"] || !names["skilled"] || names["ability_score_improvement"] {
		t.Errorf("feat options = %+v", feats)
	}

	c.Selections = []Selection{
		{FeatureID: "custom", ChoiceID: "scores", Allocated: map[string]int{"cha": 2, "str": 1}},
		{FeatureID: "custom", ChoiceID: "skills", Picked: []string{"arcana", "nature"}},
		{FeatureID: "custom", ChoiceID: "feat", Picked: []string{"alert"}},
	}
	sh = mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	for _, id := range []string{"scores", "skills", "feat"} {
		if _, ok := findPending(sh, "custom", id); ok {
			t.Errorf("%s has been chosen", id)
		}
	}
	if sh.Abilities[Cha].Score != 12 || sh.Abilities[Str].Score != 11 {
		t.Errorf("Cha %d, Str %d", sh.Abilities[Cha].Score, sh.Abilities[Str].Score)
	}
	if sh.Skills["arcana"].Level != ProfTrained || sh.Skills["nature"].Level != ProfTrained {
		t.Errorf("arcana %q, nature %q", sh.Skills["arcana"].Level, sh.Skills["nature"].Level)
	}
	if sh.Initiative != 5 { // Dex 10, plus 5 from Alert
		t.Errorf("initiative = %d", sh.Initiative)
	}

	// A feat with its own choices asks for them too.
	c.Selections[2].Picked = []string{"skilled"}
	sh = mustCompute(t, c, lib, Ruleset{})
	if _, ok := findPending(sh, "skilled@custom:feat", "skills"); !ok {
		t.Errorf("the Skilled feat should want its skills: %+v", sh.PendingChoices)
	}
}

func TestSentinelFeat(t *testing.T) {
	lib := loadExample(t)
	c := Character{ID: "s", Classes: []ClassLevel{{ClassID: "rogue", Level: 4}},
		Selections: []Selection{{FeatureID: "rogue_asi_4", ChoiceID: "feat", Picked: []string{"sentinel"}}}}

	opts, err := ChoiceOptions(c, lib, Ruleset{}, "rogue_asi_4", "feat")
	if err != nil {
		t.Fatal(err)
	}
	offered := false
	for _, o := range opts {
		offered = offered || o.OptionID == "sentinel"
	}
	if !offered {
		t.Errorf("Sentinel should be offered as a feat: %+v", opts)
	}

	// It isn't an origin feat, so the Custom background doesn't offer it.
	origin, err := ChoiceOptions(Character{ID: "o", BackgroundID: "custom"}, lib, Ruleset{}, "custom", "feat")
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range origin {
		if o.OptionID == "sentinel" {
			t.Error("Sentinel is not an origin feat")
		}
	}

	sh := mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	if _, ok := findFeature(sh, "sentinel"); !ok {
		t.Fatal("the feat is missing")
	}
	reaction := false
	for _, a := range sh.Actions {
		reaction = reaction || (a.Name == "Sentinel" && a.Type == ActionReaction && a.FeatureID == "sentinel")
	}
	if !reaction {
		t.Errorf("no Sentinel reaction in %+v", sh.Actions)
	}
}

func TestHermitBackground(t *testing.T) {
	lib := loadExample(t)
	c := Character{ID: "h", BackgroundID: "hermit", Classes: []ClassLevel{{ClassID: "rogue", Level: 1}},
		Abilities: map[Ability]AbilityScore{Con: {Score: 10}, Wis: {Score: 10}, Cha: {Score: 10}, Str: {Score: 10}}}

	sh := mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	if _, ok := findPending(sh, "hermit", "scores"); !ok {
		t.Errorf("the ability scores should be pending: %+v", sh.PendingChoices)
	}
	opts, err := ChoiceOptions(c, lib, Ruleset{}, "hermit", "scores")
	if err != nil || len(opts) != 3 || opts[0].OptionID != "con" || opts[1].OptionID != "wis" || opts[2].OptionID != "cha" {
		t.Errorf("score options = %+v (err %v)", opts, err)
	}
	for _, skill := range []string{"medicine", "religion"} {
		if sh.Skills[skill].Level != ProfTrained {
			t.Errorf("%s = %q, want trained", skill, sh.Skills[skill].Level)
		}
	}
	if sh.Tools["herbalism_kit"].Level != ProfTrained {
		t.Errorf("tools = %+v", sh.Tools)
	}
	if _, ok := findFeature(sh, "healer"); !ok {
		t.Error("the Healer feat should be on the sheet")
	}
	medic := false
	for _, a := range sh.Actions {
		medic = medic || a.Name == "Battle Medic"
	}
	if !medic {
		t.Error("Battle Medic should be an action")
	}

	c.Selections = []Selection{{FeatureID: "hermit", ChoiceID: "scores", Allocated: map[string]int{"wis": 2, "con": 1}}}
	sh = mustCompute(t, c, lib, Ruleset{})
	if sh.Abilities[Wis].Score != 12 || sh.Abilities[Con].Score != 11 || sh.Abilities[Cha].Score != 10 {
		t.Errorf("abilities = %+v", sh.Abilities)
	}
	if _, ok := findPending(sh, "hermit", "scores"); ok {
		t.Error("the ability scores are chosen")
	}

	// Healer is an origin feat, so the Custom background offers it too.
	feats, err := ChoiceOptions(Character{ID: "x", BackgroundID: "custom"}, lib, Ruleset{}, "custom", "feat")
	if err != nil {
		t.Fatal(err)
	}
	offered := false
	for _, o := range feats {
		offered = offered || o.OptionID == "healer"
	}
	if !offered {
		t.Errorf("custom feats = %+v", feats)
	}
}

func hermitRogue(picks ...string) Character {
	return Character{ID: "h", BackgroundID: "hermit", Classes: []ClassLevel{{ClassID: "rogue", Level: 1}},
		Selections: []Selection{{FeatureID: "class:rogue:start", ChoiceID: "skills", Picked: picks}}}
}

func TestBackgroundSkillReplacement(t *testing.T) {
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"skills.json": `[{"id":"medicine","name":"Medicine","default_ability":"wis"},{"id":"religion","name":"Religion","default_ability":"int"},
			{"id":"stealth","name":"Stealth","default_ability":"dex"},{"id":"arcana","name":"Arcana","default_ability":"int"}]`,
		"classes/c.json": `{"id":"rogue","name":"Rogue","hit_die":8,"levels":[["sneaky"]],"starting_choices":[
			{"id":"skills","prompt":"Skills","kind":"pick","count":2,"filter":{"type":"skill"}}]}`,
		"features/f.json": `[
			{"id":"hermit","name":"Hermit","origin":"background","tags":["background"],
			 "grants":{"skills":{"medicine":"trained","religion":"trained"}}},
			{"id":"picker","name":"Picker","origin":"background","tags":["background"],
			 "choices":[{"id":"skills","prompt":"Skills","kind":"pick","count":1,"filter":{"type":"skill"}}]},
			{"id":"sneaky","name":"Sneaky","grants":{"skills":{}}},
			{"id":"elfy","name":"Elfy","origin":"race","tags":["race"],"grants":{"skills":{"medicine":"trained"}}},
			{"id":"expert","name":"Expert","grants":{"skills":{"medicine":"expert"}}}]`,
		"items/i.json": `[{"id":"amulet","name":"Amulet","kind":"gear","grants":{"skills":{"medicine":"trained"}}}]`,
	}))
	pending := func(c Character) (PendingChoice, bool) {
		t.Helper()
		return findPending(mustCompute(t, c, lib, Ruleset{}), "hermit", SkillReplacementChoice)
	}

	if _, ok := pending(hermitRogue("stealth", "arcana")); ok {
		t.Error("no overlap, so nothing to replace")
	}

	// One overlap, with a class pick, and the prompt names it.
	c := hermitRogue("medicine", "stealth")
	p, ok := pending(c)
	if !ok || p.Remaining != 1 || !strings.Contains(p.Prompt, "Medicine") || strings.Contains(p.Prompt, "Religion") {
		t.Errorf("pending = %+v (found %v)", p, ok)
	}

	// Any skill is offered, except those already held. The API can look the choice up.
	opts, err := ChoiceOptions(c, lib, Ruleset{}, "hermit", SkillReplacementChoice)
	if err != nil {
		t.Fatal(err)
	}
	eligible := map[string]bool{}
	for _, o := range opts {
		eligible[o.OptionID] = o.Eligible
	}
	if len(opts) != 4 || !eligible["arcana"] || eligible["medicine"] || eligible["religion"] || eligible["stealth"] {
		t.Errorf("options = %+v", opts)
	}

	// Choosing grants the skill and clears the prompt, and the overlapping skill is still there.
	c.Selections = append(c.Selections, Selection{FeatureID: "hermit", ChoiceID: SkillReplacementChoice, Picked: []string{"arcana"}})
	sh := mustCompute(t, c, lib, Ruleset{})
	if _, ok := findPending(sh, "hermit", SkillReplacementChoice); ok {
		t.Errorf("the replacement is chosen: %+v", sh.PendingChoices)
	}
	for _, skill := range []string{"arcana", "medicine", "religion", "stealth"} {
		if sh.Skills[skill].Level != ProfTrained {
			t.Errorf("%s = %q", skill, sh.Skills[skill].Level)
		}
	}
	if hasWarning(sh, WarnOverCount) || hasWarning(sh, WarnNotInPool) {
		t.Errorf("warnings = %+v", sh.Warnings)
	}

	// If the overlap goes away, the replacement stops counting and doesn't warn.
	c.Selections[0].Picked = []string{"stealth"}
	sh = mustCompute(t, c, lib, Ruleset{})
	if sh.Skills["arcana"].Level == ProfTrained || hasWarning(sh, WarnOverCount) {
		t.Errorf("arcana = %q, warnings %+v", sh.Skills["arcana"].Level, sh.Warnings)
	}

	// Two overlaps need two picks, and one leaves one pending.
	c = hermitRogue("medicine", "religion")
	if p, ok := pending(c); !ok || p.Remaining != 2 || !strings.Contains(p.Prompt, "Medicine, Religion") {
		t.Errorf("two overlaps: %+v", p)
	}
	c.Selections = append(c.Selections, Selection{FeatureID: "hermit", ChoiceID: SkillReplacementChoice, Picked: []string{"arcana"}})
	if p, ok := pending(c); !ok || p.Remaining != 1 {
		t.Errorf("one of two chosen: %+v", p)
	}

	// Other places a skill can come from.
	c = Character{ID: "h", BackgroundID: "hermit", RaceID: "elfy"}
	if p, ok := pending(c); !ok || p.Remaining != 1 {
		t.Errorf("race overlap: %+v", p)
	}
	c = Character{ID: "h", BackgroundID: "hermit", Skills: map[string]ProficiencyLevel{"religion": ProfTrained}}
	if p, ok := pending(c); !ok || p.Remaining != 1 {
		t.Errorf("character's own skill: %+v", p)
	}

	// Picks on the background itself count too.
	c = Character{ID: "h", BackgroundID: "picker", RaceID: "elfy", Selections: []Selection{{FeatureID: "picker", ChoiceID: "skills", Picked: []string{"medicine"}}}}
	if p, ok := findPending(mustCompute(t, c, lib, Ruleset{}), "picker", SkillReplacementChoice); !ok || p.Remaining != 1 {
		t.Errorf("picked overlap: %+v", p)
	}

	// Expertise and items aren't overlaps.
	c = Character{ID: "h", BackgroundID: "hermit", FeatureIDs: []string{"expert"}}
	if _, ok := pending(c); ok {
		t.Error("expertise isn't an overlap")
	}
	c = Character{ID: "h", BackgroundID: "hermit", Inventory: []InventoryItem{{ID: "a", ItemID: "amulet", Quantity: 1, Equipped: true}}}
	if _, ok := pending(c); ok {
		t.Error("an item isn't an overlap")
	}

	// Only backgrounds have this choice.
	if _, err := ChoiceOptions(c, lib, Ruleset{}, "sneaky", SkillReplacementChoice); !isNotFoundErr(err) {
		t.Errorf("err = %v", err)
	}
}
