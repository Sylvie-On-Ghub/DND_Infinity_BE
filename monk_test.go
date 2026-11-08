package character

// monk_test.go - tests for the Monk and the Warrior of Mercy in content/classes.
// Skips itself if the packs aren't there.

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

// monkAt is a level N human monk with Str 10, Dex 16 (+3) and Wis 14 (+2), in the subclass from level 3.
func monkAt(level int, sub string, sel ...Selection) Character {
	cl := ClassLevel{ClassID: "monk", Level: level}
	if level >= 3 {
		cl.SubclassID = sub
	}
	return Character{
		ID: "m", Name: "Monk", RaceID: "human",
		Abilities:  map[Ability]AbilityScore{Str: {Score: 10}, Dex: {Score: 16}, Con: {Score: 14}, Int: {Score: 10}, Wis: {Score: 14}, Cha: {Score: 8}},
		Classes:    []ClassLevel{cl},
		Selections: sel,
	}
}

func TestMonkBasics(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, monkAt(1, ""), lib, Ruleset{})
	noContentWarnings(t, sh)
	if class, err := lib.Class("monk"); err != nil || class.HitDie != 8 || class.Name != "Monk" {
		t.Errorf("class = %+v (err %v)", class, err)
	}
	if !sh.Abilities[Str].SaveProf || !sh.Abilities[Dex].SaveProf || sh.Abilities[Wis].SaveProf {
		t.Errorf("saves = %+v", sh.Abilities)
	}
	weapons := slices.Clone(sh.WeaponProfs)
	slices.Sort(weapons)
	if !slices.Equal(weapons, []string{"hand_crossbow", "scimitar", "shortsword", "simple"}) || len(sh.ArmourProfs) != 0 {
		t.Errorf("weapons %v, armour %v", weapons, sh.ArmourProfs)
	}
	for choice, want := range map[string]int{"skills": 6, "tool": 27} {
		if opts, err := ChoiceOptions(monkAt(1, ""), lib, Ruleset{}, "class:monk:start", choice); err != nil || len(opts) != want {
			t.Errorf("%s options = %d (err %v), want %d", choice, len(opts), err, want)
		}
	}
	if sh.AC != 15 {
		t.Errorf("AC = %d, want 15 (10 + Dex 3 + Wis 2)", sh.AC)
	}
	a := attackNamed(t, sh, "Unarmed Strike (Martial Arts)")
	if a.Ability != Dex || a.ToHit != 5 || a.DamageDice != "1d6" || a.DamageBonus != 3 {
		t.Errorf("Martial Arts strike = %+v, want Dex, +5, 1d6+3", a)
	}
	if b, ok := actionNamed(sh, "Bonus Unarmed Strike"); !ok || b.Type != ActionBonusAction {
		t.Errorf("bonus strike = %+v", b)
	}

	c := monkAt(1, "")
	c.Inventory = []InventoryItem{{ItemID: "shield", Quantity: 1, Equipped: true}}
	if sh := mustCompute(t, c, lib, Ruleset{}); sh.AC != 15 {
		t.Errorf("with a Shield, AC = %d, want 15 (10 + Dex 3 + Shield 2): Unarmored Defense shouldn't apply", sh.AC)
	}
}

// The Part B columns of the Monk table, level by level.
func TestMonkTable(t *testing.T) {
	lib := loadElfLib(t)
	base := mustCompute(t, monkAt(1, ""), lib, Ruleset{}).Speeds["walk"]
	for level := 1; level <= 20; level++ {
		sh := mustCompute(t, monkAt(level, ""), lib, Ruleset{})
		noContentWarnings(t, sh)
		die := "1d6"
		switch {
		case level >= 17:
			die = "1d12"
		case level >= 11:
			die = "1d10"
		case level >= 5:
			die = "1d8"
		}
		if a := attackNamed(t, sh, "Unarmed Strike (Martial Arts)"); a.DamageDice != die {
			t.Errorf("level %d: Martial Arts die %+v, want %s", level, a, die)
		}
		r, ok := findResource(sh, "monk_focus")
		if ok != (level >= 2) || (ok && (r.Max != level || r.Recharge != RechargeShortRest)) {
			t.Errorf("level %d: Focus Points %+v, want %d per Short Rest", level, r, level)
		}
		bonus := 0
		for _, step := range []struct{ at, ft int }{{2, 10}, {6, 15}, {10, 20}, {14, 25}, {18, 30}} {
			if level >= step.at {
				bonus = step.ft
			}
		}
		if got := sh.Speeds["walk"]; got != base+bonus {
			t.Errorf("level %d: speed %d, want %d", level, got, base+bonus)
		}
	}
}

func TestMonkFeaturesByLevel(t *testing.T) {
	lib := loadElfLib(t)
	gained := map[string]int{
		"mercy_hand_of_harm": 3, "mercy_hand_of_healing": 3, "mercy_implements_of_mercy": 3, "mercy_physicians_touch": 6,
		"mercy_flurry_of_healing_and_harm": 11, "mercy_hand_of_ultimate_mercy": 17,
	}
	maps.Copy(gained, map[string]int{
		"monk_martial_arts": 1, "monk_unarmored_defense": 1, "monk_focus": 2, "monk_unarmored_movement": 2, "monk_uncanny_metabolism": 2,
		"monk_deflect_attacks": 3, "monk_asi_4": 4, "monk_slow_fall": 4, "monk_extra_attack": 5, "monk_stunning_strike": 5,
		"monk_empowered_strikes": 6, "monk_evasion": 7, "monk_asi_8": 8, "monk_acrobatic_movement": 9, "monk_heightened_focus": 10,
		"monk_self_restoration": 10, "monk_asi_12": 12, "monk_deflect_energy": 13, "monk_disciplined_survivor": 14,
		"monk_perfect_focus": 15, "monk_asi_16": 16, "monk_superior_defense": 18, "monk_epic_boon": 19, "monk_body_and_mind": 20,
	})
	for level := 1; level <= 20; level++ {
		sh := mustCompute(t, monkAt(level, "mercy"), lib, Ruleset{})
		for id, at := range gained {
			if _, ok := findFeature(sh, id); ok != (level >= at) {
				t.Errorf("level %d: has %s = %v, but it is gained at %d", level, id, ok, at)
			}
		}
	}
}

func TestMonkUnarmoredMovementNeedsNoArmourOrShield(t *testing.T) {
	lib := loadElfLib(t)
	free := mustCompute(t, monkAt(2, ""), lib, Ruleset{}).Speeds["walk"]
	for _, item := range []string{"leather_armor", "shield"} {
		c := monkAt(2, "")
		c.Inventory = []InventoryItem{{ItemID: item, Quantity: 1, Equipped: true}}
		if got := mustCompute(t, c, lib, Ruleset{}).Speeds["walk"]; got != free-10 {
			t.Errorf("with %s: speed %d, want %d", item, got, free-10)
		}
	}
}

func TestMonkFocusActions(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, monkAt(5, ""), lib, Ruleset{})
	for _, name := range []string{"Flurry of Blows", "Patient Defense", "Step of the Wind", "Stunning Strike"} {
		if a, ok := actionNamed(sh, name); !ok || a.ResourceID != "monk_focus" {
			t.Errorf("%s = %+v, want it to spend Focus Points", name, a)
		}
	}
	if a, _ := actionNamed(sh, "Flurry of Blows"); !strings.Contains(a.Description, "two Unarmed Strikes") {
		t.Errorf("level 5 Flurry = %q", a.Description)
	}
	if a, _ := actionNamed(sh, "Deflect Attacks"); a.Type != ActionReaction || !strings.Contains(a.Description, "Bludgeoning, Piercing or Slashing") || !strings.Contains(a.Description, "2d8") {
		t.Errorf("level 5 Deflect Attacks = %+v", a)
	}

	sh = mustCompute(t, monkAt(13, ""), lib, Ruleset{})
	if a, _ := actionNamed(sh, "Flurry of Blows"); !strings.Contains(a.Description, "three Unarmed Strikes") {
		t.Errorf("level 13 Flurry = %q", a.Description)
	}
	if a, _ := actionNamed(sh, "Patient Defense"); !strings.Contains(a.Description, "2d10") {
		t.Errorf("level 13 Patient Defense = %q", a.Description)
	}
	if a, _ := actionNamed(sh, "Deflect Attacks"); !strings.Contains(a.Description, "any damage") {
		t.Errorf("level 13 Deflect Attacks = %q", a.Description)
	}
	if r, ok := findResource(sh, "monk_uncanny_metabolism"); !ok || r.Max != 1 || r.Recharge != RechargeLongRest {
		t.Errorf("Uncanny Metabolism = %+v", r)
	}
}

func TestMonkHighLevels(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, monkAt(14, ""), lib, Ruleset{})
	for _, ab := range []Ability{Str, Dex, Con, Int, Wis, Cha} {
		if !sh.Abilities[ab].SaveProf {
			t.Errorf("Disciplined Survivor: no %s save proficiency", ab)
		}
	}
	sh = mustCompute(t, monkAt(20, ""), lib, Ruleset{})
	if sh.Abilities[Dex].Score != 20 || sh.Abilities[Wis].Score != 18 {
		t.Errorf("Body and Mind: Dex %d, Wis %d, want 20 and 18", sh.Abilities[Dex].Score, sh.Abilities[Wis].Score)
	}
	if sh.AC != 19 {
		t.Errorf("level 20 AC = %d, want 19 (10 + 5 + 4)", sh.AC)
	}
}

func TestWarriorOfMercy(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, monkAt(3, "mercy"), lib, Ruleset{})
	noContentWarnings(t, sh)
	if sh.Skills["insight"].Level != ProfTrained || sh.Skills["medicine"].Level != ProfTrained {
		t.Errorf("insight %+v, medicine %+v, want proficient", sh.Skills["insight"], sh.Skills["medicine"])
	}
	if _, ok := sh.Tools["herbalism_kit"]; !ok {
		t.Errorf("tools = %v, want the Herbalism Kit", sh.Tools)
	}
	for name, typ := range map[string]ActionType{"Hand of Harm": ActionOther, "Hand of Healing": ActionAction} {
		a, ok := actionNamed(sh, name)
		if !ok || a.Type != typ || a.ResourceID != "monk_focus" || !strings.Contains(a.Description, "1d6") {
			t.Errorf("%s = %+v, want a %s spending Focus Points with 1d6", name, a, typ)
		}
	}
	if a, _ := actionNamed(mustCompute(t, monkAt(11, "mercy"), lib, Ruleset{}), "Hand of Harm"); !strings.Contains(a.Description, "1d10") {
		t.Errorf("level 11 Hand of Harm = %q, want 1d10", a.Description)
	}

	sh = mustCompute(t, monkAt(17, "mercy"), lib, Ruleset{})
	if r, ok := findResource(sh, "mercy_flurry_of_healing_and_harm"); !ok || r.Max != 2 || r.Recharge != RechargeLongRest {
		t.Errorf("Flurry of Healing and Harm = %+v, want 2 (Wis +2) per Long Rest", r)
	}
	if r, ok := findResource(sh, "mercy_hand_of_ultimate_mercy"); !ok || r.Max != 1 || r.Recharge != RechargeLongRest {
		t.Errorf("Hand of Ultimate Mercy = %+v", r)
	}
}
