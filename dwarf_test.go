package character

// dwarf_test.go - tests for the Dwarf in content/core-races. Skips itself if the packs aren't there.

import (
	"slices"
	"testing"
)

func dwarfChar(level int) Character {
	return Character{
		ID: "d", RaceID: "dwarf",
		Abilities: map[Ability]AbilityScore{Con: {Score: 14}},
		Classes:   []ClassLevel{{ClassID: "fighter", Level: level}},
	}
}

func TestDwarfTraits(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, dwarfChar(1), lib, Ruleset{})
	noContentWarnings(t, sh)
	if sh.Speeds[SpeedWalk] != 30 || darkvision(sh) != 120 {
		t.Errorf("speed %d, darkvision %d", sh.Speeds[SpeedWalk], darkvision(sh))
	}
	for _, s := range sh.Senses {
		if s.Name == "tremorsense" {
			t.Error("Tremorsense only lasts while Stonecunning is active, so it shouldn't always be listed")
		}
	}
	if !slices.Equal(sh.Languages, []string{"common", "dwarvish"}) {
		t.Errorf("languages = %v", sh.Languages)
	}
	if !slices.Contains(sh.Resistances, DamageType("poison")) {
		t.Errorf("resistances = %v", sh.Resistances)
	}
	if sh.Abilities[Con].Score != 14 {
		t.Errorf("a race shouldn't change ability scores: Con %d", sh.Abilities[Con].Score)
	}
	found := false
	for _, n := range sh.Notes {
		if n.Kind == ModAdvantage && n.Target == "save.all" && n.Condition == "to avoid or end the Poisoned condition" {
			found = true
		}
	}
	if !found {
		t.Errorf("the advantage against poison should be a note: %+v", sh.Notes)
	}
}

func TestDwarvenToughnessAddsOneHPPerLevel(t *testing.T) {
	lib := loadElfLib(t)
	for _, level := range []int{1, 2, 5, 12, 20} {
		c := dwarfChar(level)
		dwarf := mustCompute(t, c, lib, Ruleset{})
		c.RaceID = ""
		plain := mustCompute(t, c, lib, Ruleset{})
		if got := dwarf.MaxHP - plain.MaxHP; got != level {
			t.Errorf("level %d: Dwarven Toughness adds %d HP", level, got)
		}
	}
}

func TestStonecunningUsesFollowProficiencyBonus(t *testing.T) {
	lib := loadElfLib(t)
	for _, level := range []int{1, 4, 5, 8, 9, 12, 13, 16, 17, 20} {
		sh := mustCompute(t, dwarfChar(level), lib, Ruleset{})
		uses := -1
		for _, r := range sh.Resources {
			if r.FeatureID == "dwarf_stonecunning" {
				uses = r.Max
				if r.Recharge != RechargeLongRest {
					t.Errorf("recharge = %s", r.Recharge)
				}
			}
		}
		if uses != sh.ProficiencyBonus {
			t.Errorf("level %d: %d uses, proficiency bonus %d", level, uses, sh.ProficiencyBonus)
		}
		action := false
		for _, a := range sh.Actions {
			if a.Name == "Stonecunning" && a.Type == ActionBonusAction && a.ResourceID == "dwarf_stonecunning" {
				action = true
			}
		}
		if !action {
			t.Errorf("level %d: no linked bonus action in %+v", level, sh.Actions)
		}
	}
}
