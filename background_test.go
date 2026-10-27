package character

// background_test.go - tests for backgrounds and races: features tagged "background" / "race", chosen by
// setting background_id / race_id. Written without being able to run them: a failure may be a test
// mistake as much as a code bug.

import (
	"net/http"
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
	lib := loadExample(t)
	c := Character{
		ID: "s", Name: "Newcomer", BackgroundID: "scholar", RaceID: "stonefolk",
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

	// The race: speed, darkvision, and the Powerful Build trait it grants.
	if sh.Speeds[SpeedWalk] != 25 {
		t.Errorf("walk speed = %d, want 25", sh.Speeds[SpeedWalk])
	}
	sawDark := false
	for _, s := range sh.Senses {
		if s.Name == "darkvision" && s.Range == 60 {
			sawDark = true
		}
	}
	if !sawDark {
		t.Errorf("stonefolk should have 60 foot darkvision: %+v", sh.Senses)
	}
	if sh.Carry.Capacity != 300 { // Str 10 x 15, doubled by the granted Powerful Build
		t.Errorf("carry capacity = %d, want 300", sh.Carry.Capacity)
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
	srv, store := newTestServer(t)
	saveFixture(t, store, Character{ID: "new", Name: "Newcomer"})

	rec := call(t, srv, http.MethodGet, "/api/content/backgrounds", nil)
	wantStatus(t, rec, http.StatusOK)
	var bgs []ContentSummary
	into(t, rec, &bgs)
	if len(bgs) != 4 || bgs[0].ID != "scholar" || bgs[1].ID != "veteran" || bgs[2].ID != "wanderer" || bgs[3].ID != "wayfarer" {
		t.Errorf("backgrounds = %+v, want scholar, veteran, wanderer, wayfarer", bgs)
	}

	rec = call(t, srv, http.MethodGet, "/api/content/races", nil)
	var races []ContentSummary
	into(t, rec, &races)
	if len(races) != 1 || races[0].ID != "stonefolk" {
		t.Errorf("races = %+v, want stonefolk", races)
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

	wantStatus(t, call(t, srv, http.MethodPatch, "/api/characters/new", `{"background_id":"scholar"}`), http.StatusOK)
	sh := sheetOf()
	if _, ok := findPending(sh, "character", "background"); ok {
		t.Error("the background is chosen")
	}
	if _, ok := findPending(sh, "scholar", "scores"); !ok {
		t.Errorf("the scholar's ability increase should be pending: %+v", sh.PendingChoices)
	}

	// A feat is not a background.
	wantStatus(t, call(t, srv, http.MethodPatch, "/api/characters/new", `{"background_id":"alert"}`), http.StatusOK)
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
	srv, store := newTestServer(t)
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
	wantStatus(t, call(t, srv, http.MethodPatch, "/api/characters/new", `{"race_id":"stonefolk"}`), http.StatusOK)
	sh := sheetOf()
	if _, ok := findPending(sh, "character", "race"); ok {
		t.Error("the race is chosen")
	}
	if sh.Speeds[SpeedWalk] != 25 {
		t.Errorf("walk speed = %d, want 25 from the race", sh.Speeds[SpeedWalk])
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
