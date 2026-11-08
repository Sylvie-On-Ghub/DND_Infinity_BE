package character

// warlock_test.go - tests for the Warlock in content/classes, layered on top of content/example.
// They skip themselves if either folder is missing. Written without being able to run them:
// a failure may be a test mistake as much as a code bug.

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func loadWarlockLib(t *testing.T) *ContentLibrary {
	t.Helper()
	loadExample(t) // skips if the example pack is missing: the classes pack uses its skills
	if _, err := os.Stat("content/classes"); err != nil {
		t.Skip("content/classes not found: copy the content folder next to the Go files to run this test")
	}
	lib, err := LoadDirs(LoadOptions{Strict: true}, "content/example", "content/classes")
	if err != nil {
		if lib != nil {
			dumpIssues(t, lib)
		}
		t.Fatalf("the example and classes packs failed to load together: %v", err)
	}
	return lib
}

func warlockFixture() Character {
	return Character{
		ID: "vex", Name: "Vex",
		Abilities: map[Ability]AbilityScore{
			Str: {Score: 8}, Dex: {Score: 14}, Con: {Score: 14}, Int: {Score: 10}, Wis: {Score: 12}, Cha: {Score: 16},
		},
		Classes: []ClassLevel{{ClassID: "warlock", SubclassID: "fiend", Level: 4, RolledHP: []int{8, 5, 5, 5}}},
		Selections: []Selection{
			{FeatureID: "class:warlock:start", ChoiceID: "skills", Picked: []string{"deception", "intimidation"}},
			{FeatureID: "eldritch_invocations_2", ChoiceID: "invocations", Picked: []string{"devils_sight", "armor_of_shadows"}},
			{FeatureID: "eldritch_invocations_1", ChoiceID: "invocations", Picked: []string{"eldritch_sight"}},
		},
		Spellbooks: map[string]SpellState{"class:warlock": {
			Cantrips: []string{"eldritch_blast", "chill_touch", "minor_illusion"},
			Known:    []string{"hex", "armor_of_agathys", "hellish_rebuke", "darkness", "misty_step"},
		}},
	}
}

func TestWarlockLevelFour(t *testing.T) {
	lib := loadWarlockLib(t)
	sh := mustCompute(t, warlockFixture(), lib, Ruleset{})
	noContentWarnings(t, sh)

	if sh.Level != 4 || sh.ProficiencyBonus != 2 {
		t.Errorf("level %d, PB %d; want 4 and 2", sh.Level, sh.ProficiencyBonus)
	}
	if sh.MaxHP != 31 { // 23 rolled + 4 levels x Con +2
		t.Errorf("max HP = %d, want 31", sh.MaxHP)
	}
	if sh.Abilities[Cha].SaveBonus != 5 || sh.Abilities[Wis].SaveBonus != 3 || sh.Abilities[Int].SaveProf {
		t.Errorf("saves: cha %d, wis %d, int proficient %v; want 5, 3, false",
			sh.Abilities[Cha].SaveBonus, sh.Abilities[Wis].SaveBonus, sh.Abilities[Int].SaveProf)
	}
	if sh.Skills["deception"].Bonus != 5 || sh.Skills["deception"].Level != ProfTrained {
		t.Errorf("deception = %+v, want trained at +5", sh.Skills["deception"])
	}

	// Pact magic: two slots of level 2, kept apart from the shared pool.
	if sh.PactSlots == nil || sh.PactSlots.Max != 2 || sh.PactSlots.Level != 2 {
		t.Errorf("pact slots = %+v, want 2 slots of level 2", sh.PactSlots)
	}
	if len(sh.SpellSlots) != 0 || sh.CasterLevel != 0 {
		t.Errorf("a warlock has no shared slots: %+v, caster level %d", sh.SpellSlots, sh.CasterLevel)
	}

	src, ok := findSource(sh, "class:warlock")
	if !ok {
		t.Fatalf("no warlock spell source in %+v", sh.Spellcasting)
	}
	if src.SaveDC != 13 || src.AttackBonus != 5 || src.CantripsAllowed != 3 || src.SpellsAllowed != 5 {
		t.Errorf("DC %d, attack %d, cantrips %d, spells %d; want 13, 5, 3, 5",
			src.SaveDC, src.AttackBonus, src.CantripsAllowed, src.SpellsAllowed)
	}
	// Five known spells plus the Fiend's four level 3 patron spells, which don't count against them.
	if len(src.Cantrips) != 3 || len(src.Spells) != 9 || src.Prep != PrepKnown {
		t.Errorf("cantrips %d, spells %d, prep %q; want 3, 9, known", len(src.Cantrips), len(src.Spells), src.Prep)
	}
	for _, id := range []string{"burning_hands", "command", "scorching_ray", "suggestion"} {
		if sp, ok := findSpell(src.Spells, id); !ok || !sp.Granted {
			t.Errorf("the Fiend should grant %s: %+v (found %v)", id, sp, ok)
		}
	}

	blast, ok := findSpell(src.Cantrips, "eldritch_blast")
	if !ok || !blast.Attack || blast.AttackBonus != 5 || len(blast.Effects) != 1 || blast.Effects[0].Dice != "1d10" {
		t.Errorf("eldritch blast = %+v, want a +5 attack for 1d10", blast)
	}
	rebuke, _ := findSpell(src.Spells, "hellish_rebuke")
	if rebuke.SaveDC != 13 || len(rebuke.Effects) != 1 || len(rebuke.Effects[0].Upcast) != 1 || rebuke.Effects[0].Upcast[0].Dice != "3d10" {
		t.Errorf("hellish rebuke = %+v, want DC 13 and 3d10 when cast from a level 2 pact slot", rebuke)
	}

	// Invocations bring in what they grant.
	sawSight := false
	for _, s := range sh.Senses {
		if s.Name == "devils_sight" && s.Range == 120 {
			sawSight = true
		}
	}
	if !sawSight {
		t.Errorf("Devil's Sight should give a 120 foot sense: %+v", sh.Senses)
	}
	if _, ok := findSource(sh, "feature:armor_of_shadows"); !ok {
		t.Error("Armor of Shadows should add a mage armor source")
	}
	sawBlessing := false
	for _, a := range sh.Actions {
		if a.Name == "Dark One's Blessing" {
			sawBlessing = true
		}
	}
	if !sawBlessing {
		t.Error("the Fiend patron should add Dark One's Blessing")
	}

	// Only the level 4 feat (or ability score improvement) is left to decide.
	if _, ok := findPending(sh, "warlock_asi_4", "feat"); !ok {
		t.Error("the level 4 feat choice should be pending")
	}
	for _, p := range realPending(sh) { // the fixture has no background, so "choose a background" is expected too
		if p.FeatureID != "warlock_asi_4" {
			t.Errorf("unexpected pending item: %+v", p)
		}
	}
}

func TestWarlockWithNothingChosenShowsEverythingPending(t *testing.T) {
	lib := loadWarlockLib(t)
	c := warlockFixture()
	c.Selections = nil
	c.Spellbooks = nil
	sh := mustCompute(t, c, lib, Ruleset{})

	want := []struct {
		feature, choice string
		remaining       int
	}{
		{"class:warlock:start", "skills", 2},
		{"eldritch_invocations_1", "invocations", 1},
		{"eldritch_invocations_2", "invocations", 2},
		{"spells:class:warlock", "cantrips", 3},
		{"spells:class:warlock", "known", 5},
	}
	for _, w := range want {
		p, ok := findPending(sh, w.feature, w.choice)
		if !ok || p.Remaining != w.remaining {
			t.Errorf("pending %s/%s = %+v (found %v), want %d remaining", w.feature, w.choice, p, ok, w.remaining)
		}
	}
}

func TestWarlockInvocationPool(t *testing.T) {
	lib := loadWarlockLib(t)
	opts, err := ChoiceOptions(warlockFixture(), lib, Ruleset{}, "eldritch_invocations_2", "invocations")
	if err != nil {
		t.Fatal(err)
	}
	if len(opts) != 30 || opts[0].Name != "Agonizing Blast" {
		t.Errorf("options = %+v, want the 30 tagged invocations, sorted by name", opts)
	}
	// The fixture is level 4: invocations for level 5 and up are offered but ineligible.
	tooLow := map[string]string{
		"ascendant_step": "Level 5+ Warlock", "eldritch_smite": "Level 5+ Warlock", "gaze_of_two_minds": "Level 5+ Warlock",
		"gift_of_the_depths": "Level 5+ Warlock", "investment_of_the_chain_master": "Level 5+ Warlock",
		"master_of_myriad_forms": "Level 5+ Warlock", "one_with_shadows": "Level 5+ Warlock", "thirsting_blade": "Level 5+ Warlock",
		"whispers_of_the_grave": "Level 7+ Warlock", "gift_of_the_protectors": "Level 9+ Warlock", "lifedrinker": "Level 9+ Warlock",
		"visions_of_distant_realms": "Level 9+ Warlock", "devouring_blade": "Level 12+ Warlock", "witch_sight": "Level 15+ Warlock",
	}
	for _, o := range opts {
		switch {
		case o.OptionID == "eldritch_sight": // the fixture's level 1 pick
			if o.Eligible || o.Reason != "already taken" {
				t.Errorf("eldritch_sight was taken at level 1 and should show as already taken: %+v", o)
			}
		case tooLow[o.OptionID] != "":
			if o.Eligible || o.Reason != tooLow[o.OptionID] {
				t.Errorf("%s should be ineligible with %q: %+v", o.OptionID, tooLow[o.OptionID], o)
			}
		case !o.Eligible:
			t.Errorf("%s should be eligible: %+v", o.OptionID, o)
		}
	}
}

func TestPactOfTheBladeIsAnInvocation(t *testing.T) {
	lib := loadWarlockLib(t)
	c := warlockFixture()
	c.Selections[1].Picked = []string{"devils_sight", "pact_of_the_blade"}
	sh := mustCompute(t, c, lib, Ruleset{})
	found := false
	for _, a := range sh.Actions {
		if a.Name == "Conjure Pact Weapon" && a.Type == ActionBonusAction {
			found = true
		}
	}
	if !found {
		t.Errorf("Pact of the Blade taken as an invocation should add a bonus action: %+v", sh.Actions)
	}

}

func TestPactWeaponProficiencyAndCharisma(t *testing.T) {
	lib := loadWarlockLib(t)
	c := warlockFixture() // PB 2, Cha 16, Str 8; warlocks only know simple weapons
	c.Selections[1].Picked = []string{"devils_sight", "pact_of_the_blade"}
	c.Inventory = []InventoryItem{{ItemID: "longsword", Quantity: 1, Equipped: true}}
	attack := func(c Character) AttackView {
		sh := mustCompute(t, c, lib, Ruleset{})
		for _, a := range sh.Attacks {
			if a.ItemID == "longsword" {
				return a
			}
		}
		t.Fatalf("no longsword attack: %+v", sh.Attacks)
		return AttackView{}
	}

	if a := attack(c); a.Proficient {
		t.Errorf("a warlock isn't proficient with a longsword by default: %+v", a)
	}
	c.Inventory[0].Proficient = true
	c.Inventory[0].AbilityOverride = Cha
	if a := attack(c); !a.Proficient || a.Ability != Cha || a.ToHit != 5 {
		t.Errorf("pact weapon attack = %+v, want proficient with Charisma at +5", a)
	}
}

func TestPactOfTheChainAndTomeAreInvocations(t *testing.T) {
	lib := loadWarlockLib(t)
	c := warlockFixture()
	c.Selections[1].Picked = []string{"pact_of_the_chain", "pact_of_the_tome"}
	sh := mustCompute(t, c, lib, Ruleset{})
	if chain, ok := findSource(sh, "feature:pact_of_the_chain"); !ok || len(chain.Spells) != 1 || chain.Spells[0].ID != "find_familiar" {
		t.Errorf("Pact of the Chain should add find familiar: %+v", chain)
	}
	for _, w := range sh.Warnings {
		if w.Value == "pact_of_the_tome" || w.Value == "pact_of_the_chain" {
			t.Errorf("a pact picked as an invocation should be a legal pick: %+v", w)
		}
	}
}

func TestWarlockLevelOneInvocation(t *testing.T) {
	lib := loadWarlockLib(t)
	c := warlockFixture()
	c.Classes[0].Level = 1
	c.Classes[0].RolledHP = c.Classes[0].RolledHP[:1]
	c.Selections = c.Selections[:1]
	sh := mustCompute(t, c, lib, Ruleset{})
	if p, ok := findPending(sh, "eldritch_invocations_1", "invocations"); !ok || p.Remaining != 1 {
		t.Errorf("a level 1 warlock should have one invocation to choose: %+v (found %v)", p, ok)
	}
	if _, ok := findPending(sh, "eldritch_invocations_2", "invocations"); ok {
		t.Error("the level 2 invocations should not be offered at level 1")
	}

	// Level 2+ invocations aren't eligible yet; ones without a prerequisite are.
	opts, err := ChoiceOptions(c, lib, Ruleset{}, "eldritch_invocations_1", "invocations")
	if err != nil {
		t.Fatal(err)
	}
	eligible := map[string]bool{}
	for _, o := range opts {
		eligible[o.OptionID] = o.Eligible
	}
	if eligible["devils_sight"] || eligible["agonizing_blast"] || !eligible["armor_of_shadows"] || !eligible["pact_of_the_blade"] {
		t.Errorf("level 1 eligibility = %v, want Level 2+ invocations ineligible", eligible)
	}
}

func TestHomebrewInvocationJoinsThePoolJustByBeingTagged(t *testing.T) {
	loadExample(t)
	if _, err := os.Stat("content/classes"); err != nil {
		t.Skip("content/classes not found")
	}
	homebrew := memPack("homebrew", map[string]string{
		"features/gills.json": `{"id":"gills_of_the_deep","name":"Gills of the Deep","origin":"class",
			"tags":["eldritch_invocation"],"description":"You can breathe water and gain a swimming speed.",
			"modifiers":[{"target":"speed.swim","kind":"add","value":30}]}`,
	})
	lib := mustLoad(t, LoadOptions{},
		PackSource{Name: "example", FS: os.DirFS("content/example")},
		PackSource{Name: "classes", FS: os.DirFS("content/classes")},
		homebrew)

	opts, err := ChoiceOptions(warlockFixture(), lib, Ruleset{}, "eldritch_invocations_2", "invocations")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range opts {
		if o.OptionID == "gills_of_the_deep" {
			found = true
		}
	}
	if len(opts) != 31 || !found {
		t.Errorf("options = %+v, want 31 including the homebrew invocation", opts)
	}

	// Picking it grants what it grants.
	c := warlockFixture()
	c.Selections[1].Picked = []string{"gills_of_the_deep", "devils_sight"}
	sh := mustCompute(t, c, lib, Ruleset{})
	if sh.Speeds[SpeedSwim] != 30 {
		t.Errorf("swim speed = %d, want 30 from the homebrew invocation", sh.Speeds[SpeedSwim])
	}
}

func TestLessonsOfTheFirstOnes(t *testing.T) {
	lib := loadWarlockLib(t)
	c := warlockFixture()
	c.Selections[1].Picked = []string{"lessons_of_the_first_ones", "devils_sight"}
	inst := "lessons_of_the_first_ones@eldritch_invocations_2:invocations"

	// The feat pick waits on this instance of the invocation.
	sh := mustCompute(t, c, lib, Ruleset{})
	if _, ok := findPending(sh, inst, "feat"); !ok {
		t.Fatalf("want a pending Origin feat pick on %s: %+v", inst, sh.PendingChoices)
	}

	// Only Origin feats are offered: never Ability Score Improvement.
	opts, err := ChoiceOptions(c, lib, Ruleset{}, inst, "feat")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, o := range opts {
		ids = append(ids, o.OptionID)
	}
	for _, want := range []string{"alert", "lucky", "skilled", "watchful"} {
		if !contains(ids, want) {
			t.Errorf("%s missing from the Origin feat options %v", want, ids)
		}
	}
	if contains(ids, "ability_score_improvement") {
		t.Errorf("Ability Score Improvement isn't an Origin feat: %v", ids)
	}

	// Picking Alert applies it.
	c.Selections = append(c.Selections, Selection{FeatureID: inst, ChoiceID: "feat", Picked: []string{"alert"}})
	sh = mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	if sh.Initiative != 2+5 { // Dex +2, Alert +5
		t.Errorf("initiative = %d, want 7 with Alert", sh.Initiative)
	}
}

func TestLessonsOfTheFirstOnesTakenTwice(t *testing.T) {
	lib := loadWarlockLib(t)
	c := warlockFixture()
	c.Selections[2].Picked = []string{"lessons_of_the_first_ones"} // level 1 pick
	c.Selections[1].Picked = []string{"lessons_of_the_first_ones", "devils_sight"}
	first := "lessons_of_the_first_ones@eldritch_invocations_1:invocations"
	second := "lessons_of_the_first_ones@eldritch_invocations_2:invocations"
	c.Selections = append(c.Selections,
		Selection{FeatureID: first, ChoiceID: "feat", Picked: []string{"alert"}},
	)

	// The second copy is still offered, and the feat the first one took is not.
	opts, err := ChoiceOptions(c, lib, Ruleset{}, "eldritch_invocations_2", "invocations")
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range opts {
		if o.OptionID == "lessons_of_the_first_ones" && !o.Eligible {
			t.Errorf("a repeatable invocation should stay eligible: %+v", o)
		}
	}
	feats, err := ChoiceOptions(c, lib, Ruleset{}, second, "feat")
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range feats {
		if o.OptionID == "alert" && o.Eligible {
			t.Errorf("Alert was taken by the first copy, so the second should have to choose a different feat: %+v", o)
		}
	}

	c.Selections = append(c.Selections, Selection{FeatureID: second, ChoiceID: "feat", Picked: []string{"lucky"}})
	sh := mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	var got []string
	for _, f := range sh.Features {
		if f.GrantedBy == first || f.GrantedBy == second {
			got = append(got, f.FeatureID)
		}
	}
	if len(got) != 2 || !contains(got, "alert") || !contains(got, "lucky") {
		t.Errorf("want Alert and Lucky, one from each copy: %v", got)
	}
}

func TestGreenFlameBlade(t *testing.T) {
	lib := loadWarlockLib(t)
	at := func(level int) SpellView {
		t.Helper()
		c := warlockFixture() // Cha 16: +3
		c.Classes[0].Level = level
		c.Spellbooks = map[string]SpellState{"class:warlock": {Cantrips: []string{"green_flame_blade"}}}
		sh := mustCompute(t, c, lib, Ruleset{})
		for _, w := range sh.Warnings {
			if w.Code == WarnNotInPool {
				t.Errorf("green flame blade should be on the warlock list: %s", w.Message)
			}
		}
		src, ok := findSource(sh, "class:warlock")
		if !ok {
			t.Fatal("no warlock spell source")
		}
		sp, ok := findSpell(src.Cantrips, "green_flame_blade")
		if !ok || len(sp.Effects) != 2 || sp.Attack {
			t.Fatalf("level %d: want a non-spell-attack cantrip with two effects: %+v", level, sp)
		}
		return sp
	}

	// Level 1: no extra dice; the second creature takes just the Charisma modifier.
	sp := at(1)
	if sp.Effects[0].Dice != "" || sp.Effects[1].Dice != "" || sp.Effects[1].Bonus != 3 || sp.Effects[0].Bonus != 0 {
		t.Errorf("level 1 effects = %+v", sp.Effects)
	}
	for lvl, dice := range map[int]string{5: "1d8", 11: "2d8", 17: "3d8"} {
		sp := at(lvl)
		if sp.Effects[0].Dice != dice || sp.Effects[1].Dice != dice || sp.Effects[1].Bonus != 3 {
			t.Errorf("level %d effects = %+v, want %s and %s+3", lvl, sp.Effects, dice, dice)
		}
	}
}

func TestSpellEffectNeedsDiceWithoutTiers(t *testing.T) {
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"spells/s.json": `[{"id":"bad","name":"Bad","level":0,"effects":[{"kind":"damage","dice":"","damage_type":"fire"}]}]`,
	}))
	if !lib.HasErrors() {
		t.Error("an effect with no dice and no tiers should be a content error")
	}
}

func TestPrestidigitationOnWarlockAndWizardLists(t *testing.T) {
	lib := loadWarlockLib(t)
	for _, list := range []string{"warlock", "wizard"} {
		sl, err := lib.SpellList(list)
		if err != nil {
			t.Fatal(err)
		}
		if !contains(sl.SpellIDs, "prestidigitation") {
			t.Errorf("prestidigitation missing from the %s list", list)
		}
	}
	sp, err := lib.Spell("prestidigitation")
	if err != nil || sp.Level != 0 || sp.Duration != "Up to 1 hour" {
		t.Errorf("prestidigitation = %+v, %v", sp, err)
	}
}

func TestInvisibilityIsAConcentrationSpellForWarlocks(t *testing.T) {
	lib := loadWarlockLib(t)
	c := warlockFixture()
	sp := c.Spellbooks["class:warlock"]
	sp.Known = []string{"hex", "armor_of_agathys", "hellish_rebuke", "darkness", "invisibility"}
	c.Spellbooks["class:warlock"] = sp
	sh := mustCompute(t, c, lib, Ruleset{})
	if hasWarningValue(sh, WarnNotInPool, "invisibility") {
		t.Error("invisibility should be on the warlock list")
	}
	src, _ := findSource(sh, "class:warlock")
	if v, ok := findSpell(src.Spells, "invisibility"); !ok || v.Level != 2 || !v.Concentration {
		t.Errorf("invisibility = %+v (found %v), want a level 2 concentration spell", v, ok)
	}
	wiz, err := lib.SpellList("wizard")
	if err != nil || !contains(wiz.SpellIDs, "invisibility") {
		t.Errorf("invisibility missing from the wizard list (%v)", err)
	}
}

func TestThunderStepIsALevelThreeWarlockSpell(t *testing.T) {
	lib := loadWarlockLib(t)
	c := warlockAt(5, "fiend")
	sp := c.Spellbooks["class:warlock"]
	sp.Known = []string{"hex", "armor_of_agathys", "hellish_rebuke", "darkness", "thunder_step"}
	c.Spellbooks["class:warlock"] = sp
	sh := mustCompute(t, c, lib, Ruleset{})
	if hasWarningValue(sh, WarnNotInPool, "thunder_step") || hasWarning(sh, WarnSpellTooHigh) {
		t.Errorf("thunder step should be a legal pick for a level 5 warlock: %+v", sh.Warnings)
	}
	src, _ := findSource(sh, "class:warlock")
	v, ok := findSpell(src.Spells, "thunder_step")
	if !ok || v.Level != 3 || v.SaveAbility != Con || v.SaveDC != 14 || len(v.Effects) != 1 || v.Effects[0].Dice != "3d10" {
		t.Errorf("thunder step = %+v (found %v), want level 3, Con save DC 14, 3d10", v, ok)
	}
	wiz, err := lib.SpellList("wizard")
	if err != nil || !contains(wiz.SpellIDs, "thunder_step") {
		t.Errorf("thunder step missing from the wizard list (%v)", err)
	}
}

func TestSpiritShroudIsABonusActionConcentrationSpell(t *testing.T) {
	lib := loadWarlockLib(t)
	c := warlockAt(5, "fiend")
	sp := c.Spellbooks["class:warlock"]
	sp.Known = []string{"hex", "armor_of_agathys", "hellish_rebuke", "darkness", "spirit_shroud"}
	c.Spellbooks["class:warlock"] = sp
	sh := mustCompute(t, c, lib, Ruleset{})
	if hasWarningValue(sh, WarnNotInPool, "spirit_shroud") || hasWarning(sh, WarnSpellTooHigh) {
		t.Errorf("spirit shroud should be a legal pick for a level 5 warlock: %+v", sh.Warnings)
	}
	src, _ := findSource(sh, "class:warlock")
	v, ok := findSpell(src.Spells, "spirit_shroud")
	if !ok || v.Level != 3 || !v.Concentration || len(v.Effects) != 1 || v.Effects[0].Dice != "1d8" {
		t.Errorf("spirit shroud = %+v (found %v), want a level 3 concentration spell with a 1d8 rider", v, ok)
	}
	wiz, err := lib.SpellList("wizard")
	if err != nil || !contains(wiz.SpellIDs, "spirit_shroud") {
		t.Errorf("spirit shroud missing from the wizard list (%v)", err)
	}
}

func TestDimensionDoorIsALevelFourWarlockSpell(t *testing.T) {
	lib := loadWarlockLib(t)
	c := warlockAt(7, "fiend")
	sp := c.Spellbooks["class:warlock"]
	sp.Known = []string{"hex", "armor_of_agathys", "hellish_rebuke", "darkness", "dimension_door"}
	c.Spellbooks["class:warlock"] = sp
	sh := mustCompute(t, c, lib, Ruleset{})
	if hasWarningValue(sh, WarnNotInPool, "dimension_door") || hasWarning(sh, WarnSpellTooHigh) {
		t.Errorf("dimension door should be a legal pick for a level 7 warlock: %+v", sh.Warnings)
	}
	src, _ := findSource(sh, "class:warlock")
	if v, ok := findSpell(src.Spells, "dimension_door"); !ok || v.Level != 4 || v.Concentration {
		t.Errorf("dimension door = %+v (found %v), want an instantaneous level 4 spell", v, ok)
	}
	wiz, err := lib.SpellList("wizard")
	if err != nil || !contains(wiz.SpellIDs, "dimension_door") {
		t.Errorf("dimension door missing from the wizard list (%v)", err)
	}
}

func TestDispelMagicIsALevelThreeWarlockSpell(t *testing.T) {
	lib := loadWarlockLib(t)
	c := warlockAt(5, "fiend")
	sp := c.Spellbooks["class:warlock"]
	sp.Known = []string{"hex", "armor_of_agathys", "hellish_rebuke", "darkness", "dispel_magic"}
	c.Spellbooks["class:warlock"] = sp
	sh := mustCompute(t, c, lib, Ruleset{})
	if hasWarningValue(sh, WarnNotInPool, "dispel_magic") || hasWarning(sh, WarnSpellTooHigh) {
		t.Errorf("dispel magic should be a legal pick for a level 5 warlock: %+v", sh.Warnings)
	}
	src, _ := findSource(sh, "class:warlock")
	if v, ok := findSpell(src.Spells, "dispel_magic"); !ok || v.Level != 3 || v.Concentration {
		t.Errorf("dispel magic = %+v (found %v), want an instantaneous level 3 spell", v, ok)
	}
	wiz, err := lib.SpellList("wizard")
	if err != nil || !contains(wiz.SpellIDs, "dispel_magic") {
		t.Errorf("dispel magic missing from the wizard list (%v)", err)
	}
}

func TestScryingIsALevelFiveWarlockSpell(t *testing.T) {
	lib := loadWarlockLib(t)
	c := warlockAt(9, "fiend")
	sp := c.Spellbooks["class:warlock"]
	sp.Known = []string{"hex", "armor_of_agathys", "hellish_rebuke", "darkness", "scrying"}
	c.Spellbooks["class:warlock"] = sp
	sh := mustCompute(t, c, lib, Ruleset{})
	if hasWarningValue(sh, WarnNotInPool, "scrying") || hasWarning(sh, WarnSpellTooHigh) {
		t.Errorf("scrying should be a legal pick for a level 9 warlock: %+v", sh.Warnings)
	}
	src, _ := findSource(sh, "class:warlock")
	if v, ok := findSpell(src.Spells, "scrying"); !ok || v.Level != 5 || !v.Concentration || v.SaveAbility != Wis {
		t.Errorf("scrying = %+v (found %v), want a level 5 concentration spell with a Wisdom save", v, ok)
	}
	wiz, err := lib.SpellList("wizard")
	if err != nil || !contains(wiz.SpellIDs, "scrying") {
		t.Errorf("scrying missing from the wizard list (%v)", err)
	}
}

// Tasha's Bubbling Cauldron, renamed for Temi.
func TestTemisBubblingCauldronIsAMysticArcanumOption(t *testing.T) {
	lib := loadWarlockLib(t)
	c := warlockAt(11, "fiend")
	c.Selections = append(c.Selections, Selection{FeatureID: "mystic_arcanum_6", ChoiceID: "spell", Picked: []string{"temis_bubbling_cauldron"}})
	sh := mustCompute(t, c, lib, Ruleset{})
	if hasWarningValue(sh, WarnNotInPool, "temis_bubbling_cauldron") {
		t.Errorf("the cauldron should be a level 6 warlock spell: %+v", sh.Warnings)
	}
	src, _ := findSource(sh, "feature:mystic_arcanum_6")
	if v, ok := findSpell(src.Spells, "temis_bubbling_cauldron"); !ok || v.Level != 6 || v.Name != "Temi's Bubbling Cauldron" {
		t.Errorf("cauldron = %+v (found %v), want Temi's Bubbling Cauldron at level 6", v, ok)
	}
	wiz, err := lib.SpellList("wizard")
	if err != nil || !contains(wiz.SpellIDs, "temis_bubbling_cauldron") {
		t.Errorf("the cauldron is missing from the wizard list (%v)", err)
	}
}

func TestChainFamiliarWithInvestment(t *testing.T) {
	lib := loadWarlockLib(t)
	c := warlockFixture()
	c.Classes[0].Level = 5
	c.Classes[0].RolledHP = append(c.Classes[0].RolledHP, 5)
	c.Selections[1].Picked = []string{"pact_of_the_chain", "investment_of_the_chain_master"}
	c.Selections = append(c.Selections, Selection{FeatureID: "investment_of_the_chain_master", ChoiceID: "aerial_or_aquatic", Picked: []string{"aerial"}})
	c.Pets = []Pet{
		{ID: "imp", CreatureID: "imp", SourceID: "pact_of_the_chain", Status: PetActive, CurrentHP: 21},
		{ID: "owl", CreatureID: "owl", SourceID: "find_familiar", Status: PetDismissed, CurrentHP: 1},
		{ID: "bad", CreatureID: "imp", SourceID: "find_familiar", Status: PetDismissed, CurrentHP: 21},
	}
	sh := mustCompute(t, c, lib, Ruleset{})
	chain, ok := findSource(sh, "feature:pact_of_the_chain")
	if !ok {
		t.Fatal("no Pact of the Chain spell source")
	}

	imp := petOf(t, sh, "imp")
	if len(imp.Warnings) != 0 {
		t.Errorf("imp warnings: %+v", imp.Warnings)
	}
	if imp.Speeds[SpeedFly] < 40 {
		t.Errorf("imp fly speed = %d, want at least 40 from Aerial", imp.Speeds[SpeedFly])
	}
	dc := false
	for _, n := range imp.Notes {
		if strings.Contains(n.Note, fmt.Sprintf("(%d)", chain.SaveDC)) {
			dc = true
		}
	}
	if !dc {
		t.Errorf("imp notes should show the warlock's save DC %d: %+v", chain.SaveDC, imp.Notes)
	}

	if owl := petOf(t, sh, "owl"); len(owl.Warnings) != 0 {
		t.Errorf("an owl from Find Familiar is allowed: %+v", owl.Warnings)
	}
	if bad := petOf(t, sh, "bad"); !hasPetWarning(bad, WarnPetNotAllowed) {
		t.Errorf("an imp from plain Find Familiar should warn: %+v", bad.Warnings)
	}
}
