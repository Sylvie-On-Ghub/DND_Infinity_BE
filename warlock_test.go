package character

// warlock_test.go - tests for content/warlock, layered on top of content/example.
// They skip themselves if either folder is missing. Written without being able to run them:
// a failure may be a test mistake as much as a code bug.

import (
	"os"
	"testing"
)

func loadWarlockLib(t *testing.T) *ContentLibrary {
	t.Helper()
	loadExample(t) // skips if the example pack is missing: the warlock pack uses its skills
	if _, err := os.Stat("content/warlock"); err != nil {
		t.Skip("content/warlock not found: copy the content folder next to the Go files to run this test")
	}
	lib, err := LoadDirs(LoadOptions{Strict: true}, "content/example", "content/warlock")
	if err != nil {
		if lib != nil {
			dumpIssues(t, lib)
		}
		t.Fatalf("the example and warlock packs failed to load together: %v", err)
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
	if len(src.Cantrips) != 3 || len(src.Spells) != 5 || src.Prep != PrepKnown {
		t.Errorf("cantrips %d, spells %d, prep %q; want 3, 5, known", len(src.Cantrips), len(src.Spells), src.Prep)
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
	if len(opts) != 10 || opts[0].Name != "Agonizing Blast" {
		t.Errorf("options = %+v, want the 10 tagged invocations, sorted by name", opts)
	}
	for _, o := range opts {
		switch {
		case o.OptionID == "eldritch_sight": // the fixture's level 1 pick
			if o.Eligible || o.Reason != "already taken" {
				t.Errorf("eldritch_sight was taken at level 1 and should show as already taken: %+v", o)
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
	if _, err := os.Stat("content/warlock"); err != nil {
		t.Skip("content/warlock not found")
	}
	homebrew := memPack("homebrew", map[string]string{
		"features/gift.json": `{"id":"gift_of_the_depths","name":"Gift of the Depths","origin":"class",
			"tags":["eldritch_invocation"],"description":"You can breathe water and gain a swimming speed.",
			"modifiers":[{"target":"speed.swim","kind":"add","value":30}]}`,
	})
	lib := mustLoad(t, LoadOptions{},
		PackSource{Name: "example", FS: os.DirFS("content/example")},
		PackSource{Name: "warlock", FS: os.DirFS("content/warlock")},
		homebrew)

	opts, err := ChoiceOptions(warlockFixture(), lib, Ruleset{}, "eldritch_invocations_2", "invocations")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range opts {
		if o.OptionID == "gift_of_the_depths" {
			found = true
		}
	}
	if len(opts) != 11 || !found {
		t.Errorf("options = %+v, want 11 including the homebrew invocation", opts)
	}

	// Picking it grants what it grants.
	c := warlockFixture()
	c.Selections[1].Picked = []string{"gift_of_the_depths", "devils_sight"}
	sh := mustCompute(t, c, lib, Ruleset{})
	if sh.Speeds[SpeedSwim] != 30 {
		t.Errorf("swim speed = %d, want 30 from the homebrew invocation", sh.Speeds[SpeedSwim])
	}
}

func TestWarlockBeyondTheTableWarns(t *testing.T) {
	lib := loadWarlockLib(t)
	c := warlockFixture()
	c.Classes[0].Level = 5
	c.Classes[0].RolledHP = []int{8, 5, 5, 5, 5}
	sh := mustCompute(t, c, lib, Ruleset{})
	if !hasWarning(sh, WarnMissingLevelData) {
		t.Errorf("a level 5 warlock should warn that the class table stops at 4, got %+v", sh.Warnings)
	}
}
