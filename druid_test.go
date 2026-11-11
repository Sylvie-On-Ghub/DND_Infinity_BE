package character

// druid_test.go - tests for the Druid in content/classes. It needs only that pack and content/example.

import (
	"slices"
	"strings"
	"testing"
)

func loadDruidLib(t *testing.T) *ContentLibrary {
	t.Helper()
	return loadWarlockLib(t)
}

func TestDruidSpellList(t *testing.T) {
	lib := loadDruidLib(t)
	list, err := lib.SpellList("druid")
	if err != nil {
		t.Fatal(err)
	}
	if len(list.SpellIDs) != 134 {
		t.Errorf("druid list has %d spells, want 134", len(list.SpellIDs))
	}
	have := map[string]bool{}
	for _, id := range list.SpellIDs {
		have[id] = true
		if _, err := lib.Spell(id); err != nil {
			t.Errorf("druid spell %q does not resolve", id)
		}
	}
	for _, id := range []string{"cure_wounds_2024", "moonbeam", "call_lightning", "shapechange"} {
		if !have[id] {
			t.Errorf("druid list missing %q", id)
		}
	}
}

// druidAt is a level N druid with Str 10, Dex 14, Con 14, Int 10, Wis 16 (+3).
func druidAt(level int) Character {
	return Character{
		ID: "d", Name: "Druid",
		Abilities: map[Ability]AbilityScore{Str: {Score: 10}, Dex: {Score: 14}, Con: {Score: 14}, Int: {Score: 10}, Wis: {Score: 16}, Cha: {Score: 10}},
		Classes:   []ClassLevel{{ClassID: "druid", Level: level}},
	}
}

func druidSource(t *testing.T, sh Sheet) SpellSourceView {
	t.Helper()
	for _, s := range sh.Spellcasting {
		if s.ID == "class:druid" {
			return s
		}
	}
	t.Fatalf("no druid spell source in %+v", sh.Spellcasting)
	return SpellSourceView{}
}

func TestDruidBasics(t *testing.T) {
	lib := loadDruidLib(t)
	sh := mustCompute(t, druidAt(1), lib, Ruleset{})
	noContentWarnings(t, sh)
	if class, err := lib.Class("druid"); err != nil || class.HitDie != 8 || class.Name != "Druid" {
		t.Errorf("class = %+v (err %v)", class, err)
	}
	if !sh.Abilities[Int].SaveProf || !sh.Abilities[Wis].SaveProf || sh.Abilities[Str].SaveProf {
		t.Errorf("saves = %+v", sh.Abilities)
	}
	if !slices.Contains(sh.ArmourProfs, "light") || !slices.Contains(sh.ArmourProfs, "shield") || slices.Contains(sh.ArmourProfs, "medium") {
		t.Errorf("armour = %v", sh.ArmourProfs)
	}
	if !slices.Contains(sh.WeaponProfs, "simple") || slices.Contains(sh.WeaponProfs, "martial") {
		t.Errorf("weapons = %v", sh.WeaponProfs)
	}
	if _, ok := sh.Tools["herbalism_kit"]; !ok {
		t.Errorf("tools = %v", sh.Tools)
	}
	opts, err := ChoiceOptions(druidAt(1), lib, Ruleset{}, "class:druid:start", "skills")
	if err != nil || len(opts) != 8 {
		t.Errorf("skill options = %+v (err %v)", opts, err)
	}
	if !slices.Contains(sh.Languages, "druidic") {
		t.Errorf("languages = %v", sh.Languages)
	}
	if !slices.Contains(innateSpells(sh), "speak_with_animals") {
		t.Errorf("innate spells = %v", innateSpells(sh))
	}
	for _, id := range []string{"druid_starting_equipment", "druid_spellcasting", "druidic", "druid_primal_order"} {
		if _, ok := findFeature(sh, id); !ok {
			t.Errorf("missing feature %s", id)
		}
	}
}

func TestDruidTable(t *testing.T) {
	lib := loadDruidLib(t)
	cases := []struct {
		level, cantrips, prepared, wild int
		slots                           []int
	}{
		{1, 2, 4, 0, []int{2}},
		{2, 2, 5, 2, []int{3}},
		{5, 3, 9, 2, []int{4, 3, 2}},
		{8, 3, 12, 3, []int{4, 3, 3, 2}},
		{17, 4, 19, 4, []int{4, 3, 3, 3, 2, 1, 1, 1, 1}},
		{20, 4, 22, 4, []int{4, 3, 3, 3, 3, 2, 2, 1, 1}},
	}
	for _, tc := range cases {
		sh := mustCompute(t, druidAt(tc.level), lib, Ruleset{})
		noContentWarnings(t, sh)
		got := []int{}
		for _, s := range sh.SpellSlots {
			got = append(got, s.Max)
		}
		if !slices.Equal(got, tc.slots) {
			t.Errorf("level %d: slots %v, want %v", tc.level, got, tc.slots)
		}
		src := druidSource(t, sh)
		if src.Ability != Wis || src.Prep != PrepPrepared || src.CantripsAllowed != tc.cantrips || src.SpellsAllowed != tc.prepared {
			t.Errorf("level %d: %+v, want %d cantrips and %d prepared", tc.level, src, tc.cantrips, tc.prepared)
		}
		r, ok := findResource(sh, "druid_wild_shape")
		if ok != (tc.wild > 0) || r.Max != tc.wild {
			t.Errorf("level %d: Wild Shape %+v, want %d", tc.level, r, tc.wild)
		}
		if ok && (r.ShortRestRegain != 1 || r.Recharge != RechargeLongRest) {
			t.Errorf("level %d: Wild Shape recharge %+v", tc.level, r)
		}
	}
}

func TestDruidPrimalOrder(t *testing.T) {
	lib := loadDruidLib(t)
	if opts, err := ChoiceOptions(druidAt(1), lib, Ruleset{}, "druid_primal_order", "order"); err != nil || len(opts) != 2 {
		t.Errorf("orders = %+v (err %v)", opts, err)
	}
	base := mustCompute(t, druidAt(1), lib, Ruleset{})
	sh := mustCompute(t, withPick(druidAt(1), "druid_primal_order", "order", "druid_magician"), lib, Ruleset{})
	noContentWarnings(t, sh)
	if got := druidSource(t, sh).CantripsAllowed; got != 3 {
		t.Errorf("Magician cantrips = %d, want 3", got)
	}
	for _, skill := range []string{"arcana", "nature"} {
		if got := sh.Skills[skill].Bonus - base.Skills[skill].Bonus; got != 3 {
			t.Errorf("Magician adds %d to %s, want Wis 3", got, skill)
		}
	}
	if sh.Skills["religion"].Bonus != base.Skills["religion"].Bonus {
		t.Error("Magician only helps Arcana and Nature")
	}
	sh = mustCompute(t, withPick(druidAt(1), "druid_primal_order", "order", "druid_warden"), lib, Ruleset{})
	noContentWarnings(t, sh)
	if !slices.Contains(sh.WeaponProfs, "martial") || !slices.Contains(sh.ArmourProfs, "medium") {
		t.Errorf("Warden: weapons %v, armour %v", sh.WeaponProfs, sh.ArmourProfs)
	}
	if got := druidSource(t, sh).CantripsAllowed; got != 2 {
		t.Errorf("Warden cantrips = %d", got)
	}
}

func TestDruidWildShape(t *testing.T) {
	lib := loadDruidLib(t)
	c := druidAt(2)
	sh := mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	form := shapeForm(t, sh)
	if form.ID != "wild_shape" {
		t.Fatalf("form = %+v", form)
	}
	p, ok := pendingKnown(sh)
	if !ok || p.Remaining != 4 {
		t.Fatalf("pending Known Forms = %+v, %v", p, ok)
	}
	c.Selections = []Selection{{FeatureID: "druid_wild_shape", ChoiceID: KnownFormsChoice, Picked: []string{"rat", "riding_horse", "spider", "wolf"}}}
	sh = mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	if _, ok := pendingKnown(sh); ok {
		t.Error("Known Forms should be settled")
	}
	if err := ApplyForm(&c, sh, FormAction{Start: &FormStart{ID: "wild_shape", CreatureID: "wolf"}}); err != nil {
		t.Fatal(err)
	}
	sh = mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	if sh.Shape == nil || sh.Shape.Name != "Wolf" {
		t.Fatalf("Shape = %+v", sh.Shape)
	}
	if c.TempHP != 2 {
		t.Errorf("temp HP = %d, want 2", c.TempHP)
	}
}

func TestDruidElementalFury(t *testing.T) {
	lib := loadDruidLib(t)
	if opts, err := ChoiceOptions(druidAt(7), lib, Ruleset{}, "druid_elemental_fury", "strike"); err != nil || len(opts) != 2 {
		t.Errorf("Elemental Fury options = %+v (err %v)", opts, err)
	}
	potent := withPick(druidAt(7), "druid_elemental_fury", "strike", "druid_potent_spellcasting")
	sh := mustCompute(t, potent, lib, Ruleset{})
	noContentWarnings(t, sh)
	found := false
	c := potent
	c.Spellbooks = map[string]SpellState{"class:druid": {Cantrips: []string{"produce_flame", "druidcraft"}}}
	src := druidSource(t, mustCompute(t, c, lib, Ruleset{}))
	if v, ok := findSpell(src.Cantrips, "produce_flame"); ok && len(v.Effects) > 0 {
		found = v.Effects[0].Bonus == 3
	}
	if !found {
		t.Errorf("Produce Flame should get Wis 3 from Potent Spellcasting: %+v", src.Cantrips)
	}

	dice := func(a AttackView) []string {
		out := []string{}
		for _, r := range a.DamageRiders {
			out = append(out, r.Dice)
		}
		return out
	}
	strike := withPick(druidAt(7), "druid_elemental_fury", "strike", "druid_primal_strike")
	strike.Inventory = []InventoryItem{equipped("quarterstaff")}
	sh = mustCompute(t, strike, lib, Ruleset{})
	noContentWarnings(t, sh)
	if got := dice(attackNamed(t, sh, "Quarterstaff")); !slices.Equal(got, []string{"1d8"}) {
		t.Errorf("level 7 riders = %v", got)
	}
	strike.Classes[0].Level = 15
	sh = mustCompute(t, strike, lib, Ruleset{})
	if got := dice(attackNamed(t, sh, "Quarterstaff")); !slices.Equal(got, []string{"2d8"}) {
		t.Errorf("level 15 riders = %v", got)
	}
}

func TestDruidBeastSpells(t *testing.T) {
	lib := loadDruidLib(t)
	for level, want := range map[int]bool{17: false, 18: true} {
		c := druidAt(level)
		c.Selections = []Selection{{FeatureID: "druid_wild_shape", ChoiceID: KnownFormsChoice, Picked: []string{"rat", "riding_horse", "spider", "wolf", "panther", "brown_bear", "black_bear", "ape"}}}
		sh := mustCompute(t, c, lib, Ruleset{})
		if err := ApplyForm(&c, sh, FormAction{Start: &FormStart{ID: "wild_shape", CreatureID: "wolf"}}); err != nil {
			t.Fatal(err)
		}
		sh = mustCompute(t, c, lib, Ruleset{})
		noContentWarnings(t, sh)
		if sh.Shape == nil || sh.Shape.CanCast != want {
			t.Errorf("level %d: Shape %+v, want CanCast %v", level, sh.Shape, want)
		}
	}
}

func TestDruidAllLevels(t *testing.T) {
	lib := loadDruidLib(t)
	for level := 1; level <= 20; level++ {
		c := withPick(druidAt(level), "druid_primal_order", "order", "druid_magician")
		c = withPick(c, "druid_elemental_fury", "strike", "druid_primal_strike")
		sh := mustCompute(t, c, lib, Ruleset{})
		noContentWarnings(t, sh)
	}
	gained := map[string]int{
		"druid_spellcasting": 1, "druidic": 1, "druid_primal_order": 1, "druid_wild_shape": 2, "druid_wild_companion": 2, "druid_asi_4": 4,
		"druid_wild_resurgence": 5, "druid_elemental_fury": 7, "druid_asi_8": 8, "druid_asi_12": 12, "druid_asi_16": 16,
		"druid_beast_spells": 18, "druid_epic_boon": 19, "druid_archdruid": 20,
	}
	for level := 1; level <= 20; level++ {
		sh := mustCompute(t, druidAt(level), lib, Ruleset{})
		for id, at := range gained {
			if _, ok := findFeature(sh, id); ok != (level >= at) {
				t.Errorf("level %d: has %s = %v, gained at %d", level, id, ok, at)
			}
		}
	}
}

func moonAt(level int) Character {
	c := druidAt(level)
	c.Classes[0].SubclassID = "moon"
	return c
}

// moonWolf computes a Moon druid already shaped into a wolf.
func moonWolf(t *testing.T, lib *ContentLibrary, level int) (Character, Sheet) {
	t.Helper()
	c := moonAt(level)
	c.Selections = []Selection{{FeatureID: "druid_wild_shape", ChoiceID: KnownFormsChoice, Picked: []string{"rat", "riding_horse", "spider", "wolf"}}}
	sh := mustCompute(t, c, lib, Ruleset{})
	if err := ApplyForm(&c, sh, FormAction{Start: &FormStart{ID: "wild_shape", CreatureID: "wolf"}}); err != nil {
		t.Fatal(err)
	}
	sh = mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	return c, sh
}

func TestMoonCircleForms(t *testing.T) {
	lib := loadDruidLib(t)
	c, sh := moonWolf(t, lib, 3)
	if sh.AC != 16 || sh.ACDetail.FormulaID != "moon_circle_forms" {
		t.Errorf("AC = %d via %q, want 16 via moon_circle_forms", sh.AC, sh.ACDetail.FormulaID)
	}
	form := shapeForm(t, sh)
	if form.MaxCR != "1" || form.TempHP != 9 {
		t.Errorf("MaxCR %q TempHP %d, want 1 and 9", form.MaxCR, form.TempHP)
	}
	if c.TempHP != 9 {
		t.Errorf("temp HP = %d, want 9", c.TempHP)
	}
	_, sh = moonWolf(t, lib, 9)
	if form := shapeForm(t, sh); form.MaxCR != "3" || form.TempHP != 27 {
		t.Errorf("level 9: MaxCR %q TempHP %d, want 3 and 27", form.MaxCR, form.TempHP)
	}
}

func TestMoonImprovedCircleForms(t *testing.T) {
	lib := loadDruidLib(t)
	// Wolf Con 12 is +1; Wis 16 adds 3 from level 6.
	_, sh := moonWolf(t, lib, 6)
	if got := sh.Abilities[Con].SaveBonus; got != 4 {
		t.Errorf("level 6 Con save = %d, want 4", got)
	}
	_, sh = moonWolf(t, lib, 5)
	if got := sh.Abilities[Con].SaveBonus; got != 1 {
		t.Errorf("level 5 Con save = %d, want 1", got)
	}
}

func TestMoonSpells(t *testing.T) {
	lib := loadDruidLib(t)
	has := func(level int, id string) bool {
		src := druidSource(t, mustCompute(t, moonAt(level), lib, Ruleset{}))
		for _, s := range append(slices.Clone(src.Cantrips), src.Spells...) { // Starry Wisp is a cantrip
			if s.ID == id && s.Granted {
				return true
			}
		}
		return false
	}
	for id, at := range map[string]int{
		"cure_wounds_2024": 3, "moonbeam": 3, "starry_wisp": 3, "conjure_animals": 5, "fount_of_moonlight": 7, "mass_cure_wounds": 9,
	} {
		if has(at-1, id) || !has(at, id) || !has(20, id) {
			t.Errorf("%s should be always prepared from level %d", id, at)
		}
	}
	// They don't count against the number prepared.
	base := mustCompute(t, druidAt(3), lib, Ruleset{})
	sh := mustCompute(t, moonAt(3), lib, Ruleset{})
	want, _ := findPending(base, "spells:class:druid", "prepared")
	if p, ok := findPending(sh, "spells:class:druid", "prepared"); !ok || p.Remaining != want.Remaining {
		t.Errorf("prepared pending = %+v, want %d remaining", p, want.Remaining)
	}
	// They can be cast while shaped.
	_, sh = moonWolf(t, lib, 3)
	found := false
	for _, n := range sh.Notes {
		if strings.Contains(n.Note, "Wild Shape") && strings.Contains(n.Note, "Moon") {
			found = true
		}
	}
	if !found {
		t.Errorf("no Moon spells in Wild Shape note: %+v", sh.Notes)
	}
}

func TestMoonlightStep(t *testing.T) {
	lib := loadDruidLib(t)
	r, ok := findResource(mustCompute(t, moonAt(10), lib, Ruleset{}), "moon_moonlight_step")
	if !ok || r.Max != 3 || r.Recharge != "long_rest" {
		t.Errorf("Moonlight Step = %+v (found %v), want 3 uses on a long rest", r, ok)
	}
	low := moonAt(10)
	low.Abilities[Wis] = AbilityScore{Score: 8}
	if r, ok := findResource(mustCompute(t, low, lib, Ruleset{}), "moon_moonlight_step"); !ok || r.Max != 1 {
		t.Errorf("Wis 8 Moonlight Step = %+v, want 1", r)
	}
	if _, ok := findResource(mustCompute(t, moonAt(9), lib, Ruleset{}), "moon_moonlight_step"); ok {
		t.Error("Moonlight Step should start at level 10")
	}
}

func TestMoonLunarForm(t *testing.T) {
	lib := loadDruidLib(t)
	_, sh := moonWolf(t, lib, 14)
	bite := attackNamed(t, sh, "Bite")
	found := false
	for _, r := range bite.DamageRiders {
		if r.Dice == "2d10" && strings.Contains(r.Condition, "Radiant") {
			found = true
		}
	}
	if !found {
		t.Errorf("Bite riders = %+v, want 2d10 Radiant", bite.DamageRiders)
	}
	sh = mustCompute(t, moonAt(14), lib, Ruleset{})
	noContentWarnings(t, sh)
	found = false
	for _, n := range sh.Notes {
		if strings.Contains(n.Condition, "Wild Shape is active") && strings.Contains(n.Note, "2d10") {
			found = true
		}
	}
	if !found {
		t.Errorf("unshaped: want a while-Wild-Shape note for 2d10: %+v", sh.Notes)
	}
}

func TestMoonAllLevels(t *testing.T) {
	lib := loadDruidLib(t)
	gained := map[string]int{"moon_circle_forms": 3, "moon_circle_spells": 3, "moon_improved_circle_forms": 6, "moon_moonlight_step": 10, "moon_lunar_form": 14}
	for level := 3; level <= 20; level++ {
		sh := mustCompute(t, moonAt(level), lib, Ruleset{})
		noContentWarnings(t, sh)
		for id, at := range gained {
			if _, ok := findFeature(sh, id); ok != (level >= at) {
				t.Errorf("level %d: has %s = %v, gained at %d", level, id, ok, at)
			}
		}
	}
}
