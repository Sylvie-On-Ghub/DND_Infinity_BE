package character

// emberkin_test.go - tests for content/races-example (the Emberkin race), layered on content/example.
// It doubles as the template for testing any new race: load the packs strictly, build a character with
// that race, and assert on what appears on the sheet. Skips itself if the packs aren't there.
// Written without being able to run it: a failure may be a test mistake as much as a code bug.

import (
	"os"
	"strings"
	"testing"
)

func loadEmberkinLib(t *testing.T) *ContentLibrary {
	t.Helper()
	loadExample(t) // skips if content/example is missing: the pack uses its skills
	if _, err := os.Stat("content/races-example"); err != nil {
		t.Skip("content/races-example not found: copy the content folder next to the Go files to run this test")
	}
	lib, err := LoadDirs(LoadOptions{Strict: true}, "content/example", "content/races-example")
	if err != nil {
		if lib != nil {
			dumpIssues(t, lib)
		}
		t.Fatalf("the example and races-example packs failed to load together: %v", err)
	}
	return lib
}

func emberkinChar(sel ...Selection) Character {
	return Character{
		ID: "ember", Name: "Cinder", RaceID: "emberkin",
		Abilities:  map[Ability]AbilityScore{Con: {Score: 14}, Cha: {Score: 10}},
		Selections: sel,
	}
}

func TestEmberkinTraits(t *testing.T) {
	lib := loadEmberkinLib(t)
	sh := mustCompute(t, emberkinChar(), lib, Ruleset{})
	noContentWarnings(t, sh)

	if _, ok := findPending(sh, "character", "race"); ok {
		t.Error("the race is chosen, so it shouldn't be pending")
	}

	// A race never changes ability scores: those increases come from the background.
	if sh.Abilities[Con].Score != 14 || sh.Abilities[Cha].Score != 10 {
		t.Errorf("Con %d, Cha %d; want 14 and 10, unchanged by the race", sh.Abilities[Con].Score, sh.Abilities[Cha].Score)
	}

	// Speed and senses come from modifiers on the race feature.
	if sh.Speeds[SpeedWalk] != 30 {
		t.Errorf("walk speed = %d, want 30", sh.Speeds[SpeedWalk])
	}
	sawDark := false
	for _, s := range sh.Senses {
		if s.Name == "darkvision" && s.Range == 60 {
			sawDark = true
		}
	}
	if !sawDark {
		t.Errorf("darkvision 60 missing: %+v", sh.Senses)
	}

	// Resistance and languages.
	resistsFire := false
	for _, d := range sh.Resistances {
		if d == "fire" {
			resistsFire = true
		}
	}
	if !resistsFire {
		t.Errorf("fire resistance missing: %+v", sh.Resistances)
	}
	if !contains(sh.Languages, "common") || !contains(sh.Languages, "ignan") {
		t.Errorf("languages = %v, want common and ignan", sh.Languages)
	}

	// The limited-use breath weapon: an action plus a resource that comes back on a short rest.
	sawBreath := false
	for _, a := range sh.Actions {
		if a.Name == "Ember Breath" {
			sawBreath = true
		}
	}
	if !sawBreath {
		t.Error("the Ember Breath action should be on the sheet")
	}
	foundRes := false
	for _, r := range sh.Resources {
		if r.FeatureID == "emberkin_ember_breath" && r.Max == 1 && r.Recharge == RechargeShortRest {
			foundRes = true
		}
	}
	if !foundRes {
		t.Errorf("the Ember Breath resource (1 use, short rest) is missing: %+v", sh.Resources)
	}

	// The innate cantrip: its own spell source, using Constitution.
	src, ok := findSource(sh, "feature:emberkin_kindle")
	if !ok {
		t.Fatalf("no innate spell source: %+v", sh.Spellcasting)
	}
	if src.SaveDC != 12 || src.AttackBonus != 4 { // 8+2+2 and 2+2 (PB 2, Con 14 is +2)
		t.Errorf("DC %d, attack %d; want 12 and 4", src.SaveDC, src.AttackBonus)
	}
	if flame, ok := findSpell(src.Cantrips, "kindle_flame"); !ok || !flame.Attack || len(flame.Effects) != 1 || flame.Effects[0].Dice != "1d8" {
		t.Errorf("kindle flame = %+v, want an attack cantrip for 1d8", flame)
	}
}

func TestEmberkinChoices(t *testing.T) {
	lib := loadEmberkinLib(t)
	sh := mustCompute(t, emberkinChar(), lib, Ruleset{})

	// Two decisions are left: the lineage, and the Forge Lore skill.
	if p, ok := findPending(sh, "emberkin", "lineage"); !ok || p.Remaining != 1 {
		t.Errorf("the lineage should be pending: %+v", sh.PendingChoices)
	}
	if p, ok := findPending(sh, "emberkin_forge_lore", "skill"); !ok || p.Remaining != 1 {
		t.Errorf("the Forge Lore skill should be pending: %+v", sh.PendingChoices)
	}

	opts, err := ChoiceOptions(emberkinChar(), lib, Ruleset{}, "emberkin", "lineage")
	if err != nil || len(opts) != 2 || opts[0].OptionID != "cinder" || opts[1].OptionID != "ash" {
		t.Errorf("lineage options = %+v (err %v), want cinder and ash", opts, err)
	}
	skills, err := ChoiceOptions(emberkinChar(), lib, Ruleset{}, "emberkin_forge_lore", "skill")
	if err != nil || len(skills) != 3 {
		t.Errorf("forge lore options = %+v (err %v), want the three allowed skills", skills, err)
	}
}

func TestEmberkinLineages(t *testing.T) {
	lib := loadEmberkinLib(t)
	forge := Selection{FeatureID: "emberkin_forge_lore", ChoiceID: "skill", Picked: []string{"arcana"}}

	t.Run("cinder is quick", func(t *testing.T) {
		sh := mustCompute(t, emberkinChar(
			Selection{FeatureID: "emberkin", ChoiceID: "lineage", Picked: []string{"cinder"}}, forge), lib, Ruleset{})
		if sh.Initiative != 2 { // Dex 0, plus the lineage
			t.Errorf("initiative = %d, want 2", sh.Initiative)
		}
		if sh.Skills["arcana"].Level != ProfTrained {
			t.Errorf("the Forge Lore pick should be applied: arcana is %q", sh.Skills["arcana"].Level)
		}
		if left := realPending(sh); len(left) != 0 {
			t.Errorf("everything is decided, but %+v is pending", left)
		}
	})

	t.Run("ash sees further", func(t *testing.T) {
		sh := mustCompute(t, emberkinChar(
			Selection{FeatureID: "emberkin", ChoiceID: "lineage", Picked: []string{"ash"}}, forge), lib, Ruleset{})
		got := 0
		for _, s := range sh.Senses {
			if s.Name == "darkvision" {
				got = s.Range
			}
		}
		if got != 90 { // 60 from the race, 30 from the lineage
			t.Errorf("darkvision = %d, want 90", got)
		}
	})
}

func TestEmberkinIsListedAsARace(t *testing.T) {
	lib := loadEmberkinLib(t)
	races, err := lib.List("races")
	if err != nil || len(races) != 3 || races[0].ID != "emberkin" || races[1].ID != "stonefolk" || races[2].ID != "tiefling" {
		t.Errorf("races = %+v (err %v), want emberkin, stonefolk and tiefling", races, err)
	}
	// Its traits are not races.
	if _, err := lib.Lookup("races", "emberkin_fire_resistance"); !isNotFoundErr(err) {
		t.Errorf("a racial trait must not be offered as a race, err = %v", err)
	}
}

// Ability score increases belong to backgrounds. The loader warns if a race touches them, so none of the
// example packs should trigger that warning.
func TestNoRaceChangesAbilityScores(t *testing.T) {
	lib := loadEmberkinLib(t)
	for _, is := range lib.Issues() {
		if is.Severity == SevWarning && strings.Contains(is.Message, "ability score increases come from backgrounds") {
			t.Errorf("a race changes ability scores: %s", is)
		}
	}
}

func TestLoaderWarnsWhenARaceChangesAbilityScores(t *testing.T) {
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"features/f.json": `[
			{"id":"direct","name":"Direct","origin":"race","tags":["race"],
			 "modifiers":[{"target":"ability.con","kind":"add","value":2}]},
			{"id":"tiered","name":"Tiered","origin":"race","tiers":[
			 {"min_level":1,"modifiers":[{"target":"ability.str","kind":"add","value":1}]}]},
			{"id":"optioned","name":"Optioned","origin":"race","choices":[
			 {"id":"c","prompt":"p","kind":"pick","count":1,"options":[
			  {"id":"o","name":"O","modifiers":[{"target":"ability.dex","kind":"add","value":1}]}]}]},
			{"id":"fine","name":"Fine","origin":"race","modifiers":[{"target":"speed.walk","kind":"add","value":30}]},
			{"id":"background_ok","name":"Background","origin":"background","tags":["background"],
			 "modifiers":[{"target":"ability.int","kind":"add","value":1}]}]`,
	}))

	warned := map[string]bool{}
	for _, is := range lib.Issues() {
		if is.Severity == SevWarning && strings.Contains(is.Message, "ability score increases come from backgrounds") {
			warned[is.ID] = true
		}
	}
	for _, id := range []string{"direct", "tiered", "optioned"} {
		if !warned[id] {
			t.Errorf("expected a warning on race %q, got %v", id, lib.Issues())
		}
	}
	if warned["fine"] || warned["background_ok"] {
		t.Errorf("only races that change ability scores should warn: %v", warned)
	}
	if lib.HasErrors() {
		t.Error("this is a warning, not an error")
	}
}

// A trait's spells can unlock by level: Ember Magic gives one spell at level 3 and adds another at level 5.
func TestEmberkinEmberMagicUnlocksByLevel(t *testing.T) {
	lib := loadEmberkinLib(t)
	at := func(level int) Sheet {
		c := emberkinChar()
		c.Classes = []ClassLevel{cls("rogue", level)}
		return mustCompute(t, c, lib, Ruleset{})
	}
	resourceFor := func(sh Sheet) (ResourceView, bool) {
		for _, r := range sh.Resources {
			if r.FeatureID == "feature:emberkin_embers" {
				return r, true
			}
		}
		return ResourceView{}, false
	}

	if _, ok := findSource(at(2), "feature:emberkin_embers"); ok {
		t.Error("Ember Magic shouldn't exist before level 3")
	}

	three := at(3)
	src, ok := findSource(three, "feature:emberkin_embers")
	if !ok || len(src.Spells) != 1 || src.Spells[0].ID != "scorching_burst" {
		t.Fatalf("level 3: %+v, want just scorching burst", src)
	}
	if r, ok := resourceFor(three); !ok || r.Max != 1 || r.Recharge != RechargeLongRest {
		t.Errorf("the daily use should be tracked as a resource: %+v", three.Resources)
	}

	five := at(5)
	src, ok = findSource(five, "feature:emberkin_embers")
	if !ok || len(src.Spells) != 2 {
		t.Fatalf("level 5: %+v, want scorching burst and smoldering veil", src)
	}
	if _, ok := findSpell(src.Spells, "smoldering_veil"); !ok {
		t.Error("smoldering veil should unlock at level 5")
	}
	burst, _ := findSpell(src.Spells, "scorching_burst")
	if src.Ability != Con || burst.SaveDC != 13 || len(burst.Effects) != 1 || burst.Effects[0].Dice != "2d6" { // 8 + PB 3 + Con +2
		t.Errorf("scorching burst = %+v (source ability %q), want DC 13 and 2d6 on Constitution", burst, src.Ability)
	}
	if r, ok := resourceFor(five); !ok || r.Max != 1 {
		t.Errorf("one shared daily use at level 5: %+v", five.Resources)
	}
}
