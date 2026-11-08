package character

// dragonborn_test.go - tests for the Dragonborn in content/core-races. Skips itself if the packs aren't there.

import (
	"slices"
	"strings"
	"testing"
)

func dragonbornChar(level int, ancestry string) Character {
	c := Character{
		ID: "d", RaceID: "dragonborn",
		Abilities: map[Ability]AbilityScore{Con: {Score: 14}},
		Classes:   []ClassLevel{{ClassID: "fighter", Level: level}},
	}
	if ancestry != "" {
		c.Selections = []Selection{{FeatureID: "dragonborn", ChoiceID: "ancestry", Picked: []string{ancestry}}}
	}
	return c
}

func TestDragonbornTraits(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, dragonbornChar(1, ""), lib, Ruleset{})
	if sh.Speeds[SpeedWalk] != 30 || darkvision(sh) != 60 {
		t.Errorf("speed %d, darkvision %d", sh.Speeds[SpeedWalk], darkvision(sh))
	}
	if !slices.Equal(sh.Languages, []string{"common", "draconic"}) {
		t.Errorf("languages = %v", sh.Languages)
	}
	if sh.Abilities[Con].Score != 14 {
		t.Errorf("a race shouldn't change ability scores: Con %d", sh.Abilities[Con].Score)
	}
	if p, ok := findPending(sh, "dragonborn", "ancestry"); !ok || p.Remaining != 1 {
		t.Errorf("ancestry pending = %+v (found %v)", p, ok)
	}
	if opts, err := ChoiceOptions(dragonbornChar(1, ""), lib, Ruleset{}, "dragonborn", "ancestry"); err != nil || len(opts) != 10 {
		t.Errorf("ancestors = %+v (err %v), want 10", opts, err)
	}
}

func TestDragonbornAncestries(t *testing.T) {
	lib := loadElfLib(t)
	for ancestry, dtype := range map[string]string{
		"black": "acid", "blue": "lightning", "brass": "fire", "bronze": "lightning", "copper": "acid",
		"gold": "fire", "green": "poison", "red": "fire", "silver": "cold", "white": "cold",
	} {
		sh := mustCompute(t, dragonbornChar(1, ancestry), lib, Ruleset{})
		noContentWarnings(t, sh)
		if !slices.Equal(sh.Resistances, []DamageType{DamageType(dtype)}) {
			t.Errorf("%s: resistances = %v, want %s", ancestry, sh.Resistances, dtype)
		}
		name := "Breath Weapon (" + strings.ToUpper(dtype[:1]) + dtype[1:] + ")"
		if a, ok := actionNamed(sh, name); !ok || a.ResourceID != "dragonborn_breath_weapon" {
			t.Errorf("%s: %s = %+v", ancestry, name, a)
		}
	}
}

func TestDragonbornBreathScales(t *testing.T) {
	lib := loadElfLib(t)
	for level, want := range map[int]struct {
		uses int
		dice string
	}{1: {2, "1d10"}, 4: {2, "1d10"}, 5: {3, "2d10"}, 9: {4, "2d10"}, 11: {4, "3d10"}, 13: {5, "3d10"}, 17: {6, "4d10"}, 20: {6, "4d10"}} {
		sh := mustCompute(t, dragonbornChar(level, "red"), lib, Ruleset{})
		if r, ok := findResource(sh, "dragonborn_breath_weapon"); !ok || r.Max != want.uses || r.Max != sh.ProficiencyBonus || r.Recharge != "long_rest" {
			t.Errorf("level %d: breath uses %+v, want %d (the Proficiency Bonus)", level, r, want.uses)
		}
		count := 0
		for _, a := range sh.Actions {
			if strings.HasPrefix(a.Name, "Breath Weapon") {
				count++
				if !strings.Contains(a.Description, want.dice+" Fire") {
					t.Errorf("level %d: %q, want %s", level, a.Description, want.dice)
				}
			}
		}
		if count != 1 {
			t.Errorf("level %d: %d breath actions", level, count)
		}
	}
}

func TestDragonbornFlightStartsAtFive(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, dragonbornChar(4, "gold"), lib, Ruleset{})
	if _, ok := actionNamed(sh, "Draconic Flight"); ok {
		t.Error("Draconic Flight starts at character level 5")
	}
	if _, ok := findResource(sh, "dragonborn_draconic_flight"); ok {
		t.Error("no flight uses before level 5")
	}
	sh = mustCompute(t, dragonbornChar(5, "gold"), lib, Ruleset{})
	if a, ok := actionNamed(sh, "Draconic Flight"); !ok || a.Type != "bonus_action" || a.ResourceID != "dragonborn_draconic_flight" {
		t.Errorf("Draconic Flight = %+v", a)
	}
	if r, ok := findResource(sh, "dragonborn_draconic_flight"); !ok || r.Max != 1 || r.Recharge != "long_rest" {
		t.Errorf("flight resource = %+v", r)
	}
	if sh.Speeds[SpeedFly] != 0 {
		t.Error("the fly speed is temporary, so it isn't on the sheet")
	}
}
