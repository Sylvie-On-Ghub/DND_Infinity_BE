package character

// barbarian_test.go - tests for the Barbarian in content/classes, layered on content/example and
// content/core-races (for a race with a walking speed). Skips itself if the packs aren't there.

import (
	"slices"
	"testing"
)

const barbWildHeart = "path_of_the_wild_heart"

// barbChar is an elf barbarian of the given level, on the Wild Heart path from level 3.
func barbChar(level int, sel ...Selection) Character {
	cl := ClassLevel{ClassID: "barbarian", Level: level}
	if level >= 3 {
		cl.SubclassID = barbWildHeart
	}
	return Character{
		ID: "b", RaceID: "elf",
		Abilities:  map[Ability]AbilityScore{Str: {Score: 16}, Dex: {Score: 14}, Con: {Score: 16}},
		Classes:    []ClassLevel{cl},
		Selections: sel,
	}
}

// barbNote finds a sheet note by target and kind, for the situational rules that change no number.
func barbNote(sh Sheet, target string, kind ModKind) (Modifier, bool) {
	for _, n := range sh.Notes {
		if n.Target == target && n.Kind == kind {
			return n, true
		}
	}
	return Modifier{}, false
}

func TestBarbarianStartingTraits(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, barbChar(1), lib, Ruleset{})
	noContentWarnings(t, sh)

	for _, ab := range []Ability{Str, Con} {
		if !sh.Abilities[ab].SaveProf {
			t.Errorf("%s save should be proficient", ab)
		}
	}
	if !slices.Contains(sh.WeaponProfs, "martial") || !slices.Contains(sh.ArmourProfs, "shield") || slices.Contains(sh.ArmourProfs, "heavy") {
		t.Errorf("weapons %v, armour %v; want martial and shield, not heavy", sh.WeaponProfs, sh.ArmourProfs)
	}
	if _, ok := findFeature(sh, "barbarian_starting_equipment"); !ok {
		t.Error("starting equipment should be listed")
	}
	if _, ok := findPending(sh, "class:barbarian:start", "skills"); !ok {
		t.Errorf("the two skills should be pending: %+v", sh.PendingChoices)
	}
	if _, ok := findPending(sh, "barbarian_weapon_mastery", "weapons"); !ok {
		t.Errorf("weapon mastery should be pending: %+v", sh.PendingChoices)
	}
}

func TestBarbarianMulticlassGetsOnlySomeTraits(t *testing.T) {
	lib := loadElfLib(t)
	c := barbChar(1)
	c.Classes = []ClassLevel{{ClassID: "rogue", Level: 1}, {ClassID: "barbarian", Level: 1}}
	sh := mustCompute(t, c, lib, Ruleset{})
	if sh.Abilities[Con].SaveProf {
		t.Error("multiclassing into barbarian shouldn't grant the Constitution save")
	}
	if !slices.Contains(sh.WeaponProfs, "martial") || !slices.Contains(sh.ArmourProfs, "shield") {
		t.Errorf("weapons %v, armour %v; want martial and shield", sh.WeaponProfs, sh.ArmourProfs)
	}
	if slices.Contains(sh.ArmourProfs, "medium") {
		t.Errorf("multiclassing shouldn't grant medium armour: %v", sh.ArmourProfs)
	}
}

func TestBarbarianRageScales(t *testing.T) {
	lib := loadElfLib(t)
	for _, tc := range []struct{ level, uses, damage int }{
		{1, 2, 2}, {2, 2, 2}, {3, 3, 2}, {5, 3, 2}, {6, 4, 2}, {8, 4, 2}, {9, 4, 3}, {11, 4, 3},
		{12, 5, 3}, {15, 5, 3}, {16, 5, 4}, {17, 6, 4}, {20, 6, 4},
	} {
		sh := mustCompute(t, barbChar(tc.level), lib, Ruleset{})
		found := false
		for _, r := range sh.Resources {
			if r.Name == "Rage" {
				found = true
				if r.Max != tc.uses || r.Recharge != RechargeLongRest || r.ShortRestRegain != 1 {
					t.Errorf("level %d: Rage = %+v, want %d uses, one back on a Short Rest", tc.level, r, tc.uses)
				}
			}
		}
		if !found {
			t.Errorf("level %d: no Rage resource in %+v", tc.level, sh.Resources)
		}
		if n, ok := barbNote(sh, "damage.all", ModAdd); !ok || n.Value != tc.damage || n.Condition == "" {
			t.Errorf("level %d: rage damage note = %+v, want +%d while raging", tc.level, n, tc.damage)
		}
	}
}

// Rage is only on while the player says so, so it must not change the sheet's numbers.
func TestBarbarianRageChangesNoNumbers(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, barbChar(1), lib, Ruleset{})
	if len(sh.Resistances) != 0 {
		t.Errorf("resistances = %v, want none outside a Rage", sh.Resistances)
	}
	for _, target := range []string{"resist.bludgeoning", "resist.piercing", "resist.slashing"} {
		if _, ok := barbNote(sh, target, ModAdd); !ok {
			t.Errorf("%s should be noted as situational", target)
		}
	}
	for _, target := range []string{"save.str", "check.str"} {
		if _, ok := barbNote(sh, target, ModAdvantage); !ok {
			t.Errorf("advantage on %s should be noted", target)
		}
	}
	for _, a := range sh.Attacks {
		if a.Name == "Unarmed Strike" && a.DamageBonus != 4 {
			t.Errorf("unarmed damage bonus = %d, want 4 (the flat 1 plus Strength, with no Rage damage)", a.DamageBonus)
		}
	}
}

func TestBarbarianUnarmoredDefense(t *testing.T) {
	lib := loadElfLib(t)
	c := barbChar(1)
	if sh := mustCompute(t, c, lib, Ruleset{}); sh.AC != 15 {
		t.Errorf("AC = %d, want 15 (10 + Dex 2 + Con 3)", sh.AC)
	}
	c.Inventory = []InventoryItem{{ItemID: "shield", Quantity: 1, Equipped: true}}
	if sh := mustCompute(t, c, lib, Ruleset{}); sh.AC != 17 {
		t.Errorf("AC with a shield = %d, want 17", sh.AC)
	}
}

func TestBarbarianFastMovementAndHeavyArmour(t *testing.T) {
	lib := loadElfLib(t)
	if sh := mustCompute(t, barbChar(4), lib, Ruleset{}); sh.Speeds[SpeedWalk] != 30 {
		t.Errorf("level 4 speed = %d, want 30", sh.Speeds[SpeedWalk])
	}
	c := barbChar(5)
	if sh := mustCompute(t, c, lib, Ruleset{}); sh.Speeds[SpeedWalk] != 40 {
		t.Errorf("level 5 speed = %d, want 40", sh.Speeds[SpeedWalk])
	}
	c.Inventory = []InventoryItem{{ItemID: "shield", Quantity: 1, Equipped: true}}
	if sh := mustCompute(t, c, lib, Ruleset{}); sh.Speeds[SpeedWalk] != 40 {
		t.Errorf("speed with a shield = %d, want 40", sh.Speeds[SpeedWalk])
	}
	c.Inventory = []InventoryItem{{ItemID: "plate_armor", Quantity: 1, Equipped: true}}
	c.Abilities[Str] = AbilityScore{Score: 16}
	if sh := mustCompute(t, c, lib, Ruleset{}); sh.Speeds[SpeedWalk] != 30 {
		t.Errorf("speed in heavy armour = %d, want 30", sh.Speeds[SpeedWalk])
	}
}

func TestBarbarianFeaturesArriveOnTime(t *testing.T) {
	lib := loadElfLib(t)
	for _, tc := range []struct {
		id    string
		level int
	}{
		{"barbarian_rage", 1}, {"barbarian_unarmored_defense", 1}, {"barbarian_weapon_mastery", 1},
		{"barbarian_danger_sense", 2}, {"barbarian_reckless_attack", 2}, {"barbarian_primal_knowledge", 3},
		{"barbarian_asi_4", 4}, {"barbarian_weapon_mastery_4", 4}, {"barbarian_extra_attack", 5}, {"barbarian_fast_movement", 5},
		{"barbarian_feral_instinct", 7}, {"barbarian_instinctive_pounce", 7}, {"barbarian_asi_8", 8},
		{"barbarian_brutal_strike", 9}, {"barbarian_weapon_mastery_10", 10}, {"barbarian_relentless_rage", 11},
		{"barbarian_asi_12", 12}, {"barbarian_improved_brutal_strike", 13}, {"barbarian_persistent_rage", 15},
		{"barbarian_asi_16", 16}, {"barbarian_improved_brutal_strike_17", 17}, {"barbarian_indomitable_might", 18},
		{"barbarian_epic_boon", 19}, {"barbarian_primal_champion", 20},
		{"wild_heart_animal_speaker", 3}, {"wild_heart_rage_of_the_wilds", 3}, {"wild_heart_aspect_of_the_wilds", 6},
		{"wild_heart_nature_speaker", 10}, {"wild_heart_power_of_the_wilds", 14},
	} {
		if _, ok := findFeature(mustCompute(t, barbChar(tc.level), lib, Ruleset{}), tc.id); !ok {
			t.Errorf("%s should arrive at level %d", tc.id, tc.level)
		}
		if tc.level > 1 {
			if _, ok := findFeature(mustCompute(t, barbChar(tc.level-1), lib, Ruleset{}), tc.id); ok {
				t.Errorf("%s shouldn't arrive before level %d", tc.id, tc.level)
			}
		}
	}
}

func TestBarbarianSituationalAdvantages(t *testing.T) {
	lib := loadElfLib(t)
	if _, ok := barbNote(mustCompute(t, barbChar(1), lib, Ruleset{}), "save.dex", ModAdvantage); ok {
		t.Error("Danger Sense shouldn't apply at level 1")
	}
	if _, ok := barbNote(mustCompute(t, barbChar(2), lib, Ruleset{}), "save.dex", ModAdvantage); !ok {
		t.Error("Danger Sense should give advantage on Dexterity saves")
	}
	if _, ok := barbNote(mustCompute(t, barbChar(7), lib, Ruleset{}), "initiative", ModAdvantage); !ok {
		t.Error("Feral Instinct should give advantage on Initiative")
	}
}

func TestBarbarianPrimalChampion(t *testing.T) {
	lib := loadElfLib(t)
	c := barbChar(20)
	c.Abilities = map[Ability]AbilityScore{Str: {Score: 20}, Con: {Score: 18}}
	sh := mustCompute(t, c, lib, Ruleset{})
	if sh.Abilities[Str].Score != 24 || sh.Abilities[Con].Score != 22 {
		t.Errorf("Str %d, Con %d; want 24 and 22", sh.Abilities[Str].Score, sh.Abilities[Con].Score)
	}
}

func TestWildHeartIsTheOnlyPathAndGrantsRitualSpells(t *testing.T) {
	lib := loadElfLib(t)
	if subs := lib.classes["barbarian"].Subclasses; len(subs) != 1 || subs[0].ID != barbWildHeart {
		t.Errorf("subclasses = %+v, want only Path of the Wild Heart", subs)
	}

	c := barbChar(3)
	c.Classes[0].SubclassID = ""
	if _, ok := findPending(mustCompute(t, c, lib, Ruleset{}), "class:barbarian", "subclass"); !ok {
		sh := mustCompute(t, c, lib, Ruleset{})
		t.Errorf("the subclass should be pending at level 3: %+v", sh.PendingChoices)
	}

	spells := func(level int) []SpellView {
		var out []SpellView
		sh := mustCompute(t, barbChar(level), lib, Ruleset{})
		noContentWarnings(t, sh)
		for _, s := range sh.Spellcasting {
			if s.Ability != Wis {
				t.Errorf("level %d: %s casts with %s, want Wisdom", level, s.ID, s.Ability)
			}
			out = append(out, s.Spells...)
		}
		return out
	}
	lvl3 := spells(3)
	for _, id := range []string{"beast_sense", "speak_with_animals"} {
		if sp, ok := findSpell(lvl3, id); !ok || !sp.Ritual {
			t.Errorf("level 3: %s = %+v, want a ritual spell", id, sp)
		}
	}
	if _, ok := findSpell(lvl3, "commune_with_nature"); ok {
		t.Error("Commune with Nature shouldn't arrive until level 10")
	}
	if sp, ok := findSpell(spells(10), "commune_with_nature"); !ok || !sp.Ritual {
		t.Errorf("level 10: commune_with_nature = %+v, want a ritual spell", sp)
	}
}

func TestWildHeartAspects(t *testing.T) {
	lib := loadElfLib(t)
	aspect := func(id string) Sheet {
		return mustCompute(t, barbChar(6, Selection{FeatureID: "wild_heart_aspect_of_the_wilds", ChoiceID: "aspect", Picked: []string{id}}), lib, Ruleset{})
	}
	if _, ok := findPending(mustCompute(t, barbChar(6), lib, Ruleset{}), "wild_heart_aspect_of_the_wilds", "aspect"); !ok {
		t.Error("the aspect should be pending at level 6")
	}
	// The elf already has Darkvision 60, so Owl takes it to 120.
	sawDark := false
	for _, s := range aspect("owl").Senses {
		sawDark = sawDark || (s.Name == "darkvision" && s.Range == 120)
	}
	if !sawDark {
		t.Errorf("Owl senses = %+v, want Darkvision 120", aspect("owl").Senses)
	}
	if got := aspect("panther").Speeds[SpeedClimb]; got != 40 {
		t.Errorf("Panther climb speed = %d, want 40, matching the walking speed", got)
	}
	if got := aspect("salmon").Speeds[SpeedSwim]; got != 40 {
		t.Errorf("Salmon swim speed = %d, want 40, matching the walking speed", got)
	}
}
