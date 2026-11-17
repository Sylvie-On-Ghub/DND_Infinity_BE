package character

// tabaxi_test.go - tests for the Tabaxi in content/homebrew, layered on content/example and
// content/classes. Skips itself if the packs aren't there.

import (
	"slices"
	"testing"
)

func tabaxiChar() Character {
	return Character{
		ID: "t", RaceID: "tabaxi",
		Abilities: map[Ability]AbilityScore{Str: {Score: 16}, Dex: {Score: 14}},
		Classes:   []ClassLevel{{ClassID: "rogue", Level: 1, RolledHP: []int{8}}},
	}
}

func TestTabaxi(t *testing.T) {
	sh := mustCompute(t, tabaxiChar(), loadSharkinLib(t), Ruleset{})
	noContentWarnings(t, sh)
	if sh.Speeds[SpeedWalk] != 30 || sh.Speeds[SpeedClimb] != 30 {
		t.Errorf("speeds = %v, want walk 30 and climb 30", sh.Speeds)
	}
	if !slices.Contains(sh.Senses, Sense{Name: "darkvision", Range: 60}) {
		t.Errorf("senses = %+v, want darkvision 60", sh.Senses)
	}
	for _, sk := range []string{"perception", "stealth"} {
		if sh.Skills[sk].Level != ProfTrained {
			t.Errorf("%s = %+v, want trained", sk, sh.Skills[sk])
		}
	}
	a, ok := findAttack(sh, "Claws")
	if !ok || a.Ability != Str || a.ToHit != 5 || a.DamageDice != "1d6" || a.DamageBonus != 3 || a.DamageType != "slashing" {
		t.Errorf("claws = %+v (found %v), want Str, +5 to hit, 1d6+3 slashing", a, ok)
	}
	if a, ok := actionNamed(sh, "Feline Agility"); !ok || a.Type != ActionFree {
		t.Errorf("feline agility = %+v (found %v)", a, ok)
	}
}
