package character

// ranger_test.go - tests for the Ranger and its Beast Master and Hunter subclasses in content/classes,
// layered on top of content/example. They skip themselves if either folder is missing.

import (
	"slices"
	"strings"
	"testing"
)

// rangerChar is a ranger of the given level with Dex 16, Wis 16 and Con 14, and a rolled HP entry per level.
func rangerChar(level int, subclass string) Character {
	cl := ClassLevel{ClassID: "ranger", SubclassID: subclass, Level: level}
	for i := 0; i < level; i++ {
		cl.RolledHP = append(cl.RolledHP, 6)
	}
	cl.RolledHP[0] = 10
	return Character{
		ID: "ranger", Name: "Ranger",
		Abilities: map[Ability]AbilityScore{
			Str: {Score: 10}, Dex: {Score: 16}, Con: {Score: 14}, Int: {Score: 10}, Wis: {Score: 16}, Cha: {Score: 8},
		},
		Classes: []ClassLevel{cl},
		Selections: []Selection{
			{FeatureID: "class:ranger:start", ChoiceID: "skills", Picked: []string{"perception", "stealth", "survival"}},
		},
	}
}

// slotList is the slot maximums in spell level order.
func slotList(sh Sheet) []int {
	var out []int
	for _, s := range sh.SpellSlots {
		out = append(out, s.Max)
	}
	return out
}

func noteContains(notes []Modifier, sub string) bool {
	return slices.ContainsFunc(notes, func(m Modifier) bool { return strings.Contains(m.Note, sub) })
}

func TestRangerLevelOne(t *testing.T) {
	lib := loadWarlockLib(t)
	sh := mustCompute(t, rangerChar(1, ""), lib, Ruleset{})
	noContentWarnings(t, sh)

	if sh.MaxHP != 12 { // 10 + Con 2
		t.Errorf("max HP = %d, want 12", sh.MaxHP)
	}
	if !sh.Abilities[Str].SaveProf || !sh.Abilities[Dex].SaveProf || sh.Abilities[Wis].SaveProf {
		t.Errorf("saves: %+v", sh.Abilities)
	}
	if got := slotList(sh); !slices.Equal(got, []int{2}) {
		t.Errorf("slots = %v, want [2]", got)
	}
	src, ok := findSource(sh, "class:ranger")
	if !ok || src.Ability != Wis || src.SpellsAllowed != 2 || src.SaveDC != 13 {
		t.Errorf("ranger spellcasting = %+v (found %v), want Wis, 2 prepared, DC 13", src, ok)
	}
	if fe, ok := findSource(sh, "feature:ranger_favored_enemy"); !ok || len(fe.Spells) != 1 || fe.Spells[0].ID != "hunters_mark" {
		t.Errorf("Favored Enemy should give Hunter's Mark: %+v", fe)
	}
	if r, ok := findResource(sh, "ranger_favored_enemy"); !ok || r.Max != 2 || r.Recharge != RechargeLongRest {
		t.Errorf("Favored Enemy uses = %+v", r)
	}
	if p, ok := findPending(sh, "ranger_weapon_mastery", "weapons"); !ok || p.Remaining != 2 {
		t.Errorf("weapon mastery pending = %+v (found %v)", p, ok)
	}
}

func TestRangerSpellsAndFavoredEnemyScale(t *testing.T) {
	lib := loadWarlockLib(t)
	cases := []struct {
		level, prepared, favored int
		slots                    []int
	}{
		{2, 3, 2, []int{2}},
		{5, 6, 3, []int{4, 2}},
		{9, 9, 4, []int{4, 3, 2}},
		{13, 11, 5, []int{4, 3, 3, 1}},
		{17, 14, 6, []int{4, 3, 3, 3, 1}},
		{20, 15, 6, []int{4, 3, 3, 3, 2}},
	}
	for _, tc := range cases {
		sh := mustCompute(t, rangerChar(tc.level, "hunter"), lib, Ruleset{})
		if got := slotList(sh); !slices.Equal(got, tc.slots) {
			t.Errorf("level %d slots = %v, want %v", tc.level, got, tc.slots)
		}
		if src, _ := findSource(sh, "class:ranger"); src.SpellsAllowed != tc.prepared {
			t.Errorf("level %d prepared = %d, want %d", tc.level, src.SpellsAllowed, tc.prepared)
		}
		if r, _ := findResource(sh, "ranger_favored_enemy"); r.Max != tc.favored {
			t.Errorf("level %d Favored Enemy uses = %d, want %d", tc.level, r.Max, tc.favored)
		}
	}
}

func TestRangerHighLevelFeatures(t *testing.T) {
	lib := loadWarlockLib(t)
	sh := mustCompute(t, rangerChar(20, "hunter"), lib, Ruleset{})
	noContentWarnings(t, sh)
	if !noteContains(sh.Notes, "attack twice") {
		t.Error("Extra Attack note missing")
	}
	if !slices.Contains(sh.Senses, Sense{Name: "blindsight", Range: 30}) {
		t.Errorf("senses = %+v, want blindsight 30", sh.Senses)
	}
	if !noteContains(sh.Notes, "d10") {
		t.Error("Foe Slayer note missing")
	}
	for _, id := range []string{"ranger_relentless_hunter", "ranger_natures_veil", "ranger_precise_hunter", "ranger_epic_boon"} {
		if !slices.ContainsFunc(sh.Features, func(f FeatureView) bool { return f.FeatureID == id }) {
			t.Errorf("level 20 ranger is missing %s", id)
		}
	}
	if r, ok := findResource(sh, "ranger_natures_veil"); !ok || r.Max != 3 {
		t.Errorf("Nature's Veil uses = %+v, want 3 (Wis)", r)
	}
}

func TestRangerRoving(t *testing.T) {
	lib := loadWarlockLib(t)
	c := rangerChar(6, "hunter")
	c.Modifiers = []Modifier{{Target: SpeedTarget(SpeedWalk), Kind: ModSet, Value: 30}} // a race's walking speed
	sh := mustCompute(t, c, lib, Ruleset{})
	if sh.Speeds[SpeedWalk] != 40 || sh.Speeds[SpeedClimb] != 40 || sh.Speeds[SpeedSwim] != 40 {
		t.Errorf("roving speeds = %v, want walk, climb and swim 40", sh.Speeds)
	}
	c.Inventory = []InventoryItem{{ItemID: "chain_mail", Quantity: 1, Equipped: true}}
	if sh := mustCompute(t, c, lib, Ruleset{}); sh.Speeds[SpeedWalk] != 30 || sh.Speeds[SpeedClimb] != 30 {
		t.Errorf("in heavy armour: %v, want walk and climb 30", sh.Speeds)
	}
}

func TestRangerFightingStyleOrDruidicWarrior(t *testing.T) {
	lib := loadWarlockLib(t)
	c := rangerChar(2, "")
	sh := mustCompute(t, c, lib, Ruleset{})
	if _, ok := findPending(sh, "ranger_fighting_style", "style"); !ok {
		t.Error("a level 2 ranger should have a fighting style to choose")
	}

	c.Selections = append(c.Selections, Selection{FeatureID: "ranger_fighting_style", ChoiceID: "druidic_warrior", Picked: []string{"guidance", "starry_wisp"}})
	sh = mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	if _, ok := findPending(sh, "ranger_fighting_style", "style"); ok {
		t.Error("Druidic Warrior replaces the fighting style feat")
	}
	dw, ok := findSource(sh, "feature:ranger_fighting_style")
	if !ok || dw.Ability != Wis || len(dw.Spells)+len(dw.Cantrips) != 2 {
		t.Errorf("Druidic Warrior cantrips = %+v (found %v)", dw, ok)
	}

	c.Selections[len(c.Selections)-1] = Selection{FeatureID: "ranger_fighting_style", ChoiceID: "druidic_warrior", Picked: []string{"guidance", "fireball"}}
	if _, ok := findWarning(mustCompute(t, c, lib, Ruleset{}), WarnNotInPool); !ok {
		t.Error("Druidic Warrior only offers druid cantrips")
	}
}

func TestRangerDeftExplorer(t *testing.T) {
	lib := loadWarlockLib(t)
	c := rangerChar(2, "")
	c.Selections = append(c.Selections,
		Selection{FeatureID: "ranger_deft_explorer", ChoiceID: "expertise", Picked: []string{"stealth"}},
		Selection{FeatureID: "ranger_deft_explorer", ChoiceID: "languages", Picked: []string{"elvish", "sylvan"}})
	sh := mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	if sh.Skills["stealth"].Level != ProfExpert {
		t.Errorf("stealth = %+v, want expertise", sh.Skills["stealth"])
	}
	if !slices.Contains(sh.Languages, "elvish") || !slices.Contains(sh.Languages, "sylvan") {
		t.Errorf("languages = %v", sh.Languages)
	}
}

func TestRangerTireless(t *testing.T) {
	lib := loadWarlockLib(t)
	c := rangerChar(10, "hunter")
	c.Exhaustion = 2
	sh := mustCompute(t, c, lib, Ruleset{})
	if r, ok := findResource(sh, "ranger_tireless"); !ok || r.Max != 3 {
		t.Errorf("Tireless uses = %+v, want 3 (Wis)", r)
	}
	if err := ApplyRest(&c, sh, "short"); err != nil || c.Exhaustion != 1 {
		t.Errorf("Tireless short rest: exhaustion %d, err %v; want 1", c.Exhaustion, err)
	}
}

func TestHunterFeatures(t *testing.T) {
	lib := loadWarlockLib(t)
	c := rangerChar(15, "hunter")
	c.Selections = append(c.Selections,
		Selection{FeatureID: "hunter_hunters_prey", ChoiceID: "prey", Picked: []string{"colossus_slayer"}},
		Selection{FeatureID: "hunter_defensive_tactics", ChoiceID: "tactics", Picked: []string{"multiattack_defense"}})
	c.Inventory = []InventoryItem{{ItemID: "longbow", Quantity: 1, Equipped: true}}
	sh := mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	bow := slices.IndexFunc(sh.Attacks, func(a AttackView) bool { return a.Name == "Longbow" })
	if bow < 0 || !slices.ContainsFunc(sh.Attacks[bow].DamageRiders, func(r Rider) bool {
		return r.Dice == "1d8" && strings.Contains(r.Condition, "Colossus Slayer")
	}) {
		t.Errorf("Colossus Slayer should be a 1d8 damage rider on the longbow: %+v", sh.Attacks)
	}
	if !noteContains(sh.Notes, "Multiattack Defense") {
		t.Error("Multiattack Defense note missing")
	}
	if !slices.ContainsFunc(sh.Actions, func(a Action) bool { return a.Name == "Superior Hunter's Defense" && a.Type == ActionReaction }) {
		t.Error("Superior Hunter's Defense reaction missing")
	}
	for _, id := range []string{"hunter_hunters_lore", "hunter_superior_hunters_prey"} {
		if !slices.ContainsFunc(sh.Features, func(f FeatureView) bool { return f.FeatureID == id }) {
			t.Errorf("missing %s", id)
		}
	}
}

func TestBeastMasterPrimalCompanion(t *testing.T) {
	lib := loadWarlockLib(t)
	c := rangerChar(3, "beast_master")
	c.Pets = []Pet{
		{ID: "land", Name: "Bramble", CreatureID: "beast_of_the_land", SourceID: "beast_master_primal_companion", Status: PetActive, CurrentHP: 20},
		{ID: "sea", CreatureID: "beast_of_the_sea", SourceID: "beast_master_primal_companion", Status: PetDismissed, CurrentHP: 20},
		{ID: "sky", CreatureID: "beast_of_the_sky", SourceID: "beast_master_primal_companion", Status: PetDismissed, CurrentHP: 16},
	}
	sh := mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	src, _ := findSource(sh, "class:ranger")

	land := petOf(t, sh, "land")
	if len(land.Warnings) != 0 {
		t.Errorf("land beast warnings: %+v", land.Warnings)
	}
	if land.AC != 16 || land.MaxHP != 20 || land.PB != 2 || land.Speeds[SpeedClimb] != 40 {
		t.Errorf("land beast: AC %d, HP %d, PB %d, speeds %v; want 16, 20, 2, climb 40", land.AC, land.MaxHP, land.PB, land.Speeds)
	}
	if a := land.Attacks[0]; a.ToHit != src.AttackBonus || a.Damage != "1d8+5" {
		t.Errorf("Beast's Strike = %+v, want +%d and 1d8+5", a, src.AttackBonus)
	}
	if land.Abilities[Wis].SaveBonus != 4 { // +2 Wis, +2 from Primal Bond
		t.Errorf("Primal Bond save = %d, want 4", land.Abilities[Wis].SaveBonus)
	}

	sea := petOf(t, sh, "sea")
	if !strings.Contains(sea.Attacks[0].Description, "DC 13") || sea.Speeds[SpeedSwim] != 60 {
		t.Errorf("sea beast: %q, speeds %v", sea.Attacks[0].Description, sea.Speeds)
	}
	sky := petOf(t, sh, "sky")
	if sky.MaxHP != 16 || sky.Attacks[0].Damage != "1d4+6 slashing" || sky.Speeds[SpeedFly] != 60 {
		t.Errorf("sky beast: HP %d, %q, %v", sky.MaxHP, sky.Attacks[0].Damage, sky.Speeds)
	}
	if !slices.ContainsFunc(sh.Actions, func(a Action) bool { return a.Name == "Command Companion" }) {
		t.Error("Command Companion action missing")
	}

	c.Classes[0].Level, c.Classes[0].RolledHP = 11, append(c.Classes[0].RolledHP, 6, 6, 6, 6, 6, 6, 6, 6)
	land = petOf(t, mustCompute(t, c, lib, Ruleset{}), "land")
	if land.MaxHP != 60 || !noteContains(land.Notes, "Force") || !noteContains(land.Notes, "twice") {
		t.Errorf("level 11 land beast: HP %d, notes %+v; want 60 and Exceptional Training and Bestial Fury notes", land.MaxHP, land.Notes)
	}
}

func TestBeastMasterOnlyAllowsPrimalBeasts(t *testing.T) {
	lib := loadWarlockLib(t)
	c := rangerChar(3, "beast_master")
	c.Pets = []Pet{{ID: "o", CreatureID: "owl", SourceID: "beast_master_primal_companion", Status: PetActive, CurrentHP: 1}}
	if p := petOf(t, mustCompute(t, c, lib, Ruleset{}), "o"); !hasPetWarning(p, WarnPetNotAllowed) {
		t.Errorf("an owl isn't a primal companion: %+v", p.Warnings)
	}
}
