package character

// orc_test.go - tests for the Orc in content/core-races. Skips itself if the packs aren't there.

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func orcChar(level int) Character {
	return Character{
		ID: "o", RaceID: "orc",
		Abilities: map[Ability]AbilityScore{Str: {Score: 16}},
		Classes:   []ClassLevel{{ClassID: "fighter", Level: level}},
	}
}

func TestOrcTraits(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, orcChar(1), lib, Ruleset{})
	noContentWarnings(t, sh)
	if sh.Speeds[SpeedWalk] != 30 || darkvision(sh) != 120 {
		t.Errorf("speed %d, darkvision %d", sh.Speeds[SpeedWalk], darkvision(sh))
	}
	if !slices.Equal(sh.Languages, []string{"common", "orc"}) {
		t.Errorf("languages = %v", sh.Languages)
	}
	if sh.Abilities[Str].Score != 16 {
		t.Errorf("a race shouldn't change ability scores: Str %d", sh.Abilities[Str].Score)
	}
	if r, ok := findResource(sh, "orc_relentless_endurance"); !ok || r.Max != 1 || r.Recharge != "long_rest" {
		t.Errorf("Relentless Endurance uses %+v", r)
	}
	if a, ok := actionNamed(sh, "Relentless Endurance"); !ok || a.ResourceID != "orc_relentless_endurance" {
		t.Errorf("Relentless Endurance = %+v", a)
	}
}

func TestOrcAdrenalineRushScales(t *testing.T) {
	lib := loadElfLib(t)
	for _, level := range []int{1, 4, 5, 9, 13, 17, 20} {
		sh := mustCompute(t, orcChar(level), lib, Ruleset{})
		pb := sh.ProficiencyBonus
		if r, ok := findResource(sh, "orc_adrenaline_rush"); !ok || r.Max != pb || r.Recharge != "short_rest" {
			t.Errorf("level %d: uses %+v, want %d per short rest", level, r, pb)
		}
		a, ok := actionNamed(sh, "Adrenaline Rush")
		if !ok || a.Type != "bonus_action" || a.ResourceID != "orc_adrenaline_rush" || !strings.Contains(a.Description, fmt.Sprintf("gain %d Temporary", pb)) {
			t.Errorf("level %d: Adrenaline Rush = %+v, want %d Temporary Hit Points", level, a, pb)
		}
	}
}
