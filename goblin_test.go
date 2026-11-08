package character

// goblin_test.go - tests for the Goblin in content/core-races. Skips itself if the packs aren't there.

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func goblinChar(level int) Character {
	return Character{
		ID: "g", RaceID: "goblin",
		Abilities: map[Ability]AbilityScore{Dex: {Score: 16}},
		Classes:   []ClassLevel{{ClassID: "fighter", Level: level}},
	}
}

func TestGoblinTraits(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, goblinChar(1), lib, Ruleset{})
	noContentWarnings(t, sh)
	if sh.Speeds[SpeedWalk] != 30 || darkvision(sh) != 60 {
		t.Errorf("speed %d, darkvision %d", sh.Speeds[SpeedWalk], darkvision(sh))
	}
	if !slices.Equal(sh.Languages, []string{"common", "goblin"}) {
		t.Errorf("languages = %v", sh.Languages)
	}
	if sh.Abilities[Dex].Score != 16 {
		t.Errorf("a race shouldn't change ability scores: Dex %d", sh.Abilities[Dex].Score)
	}
	if n, ok := barbNote(sh, TargetSaveAll, ModAdvantage); !ok || !strings.Contains(n.Condition, "Charmed") {
		t.Errorf("Fey Ancestry = %+v (found %v)", n, ok)
	}
	if a, ok := actionNamed(sh, "Nimble Escape"); !ok || a.Type != "bonus_action" || a.ResourceID != "" {
		t.Errorf("Nimble Escape = %+v, want an unlimited Bonus Action", a)
	}
}

func TestGoblinFuryOfTheSmallScales(t *testing.T) {
	lib := loadElfLib(t)
	for _, level := range []int{1, 4, 5, 9, 13, 17, 20} {
		sh := mustCompute(t, goblinChar(level), lib, Ruleset{})
		pb := sh.ProficiencyBonus
		if r, ok := findResource(sh, "goblin_fury_of_the_small"); !ok || r.Max != pb || r.Recharge != "long_rest" {
			t.Errorf("level %d: uses %+v, want %d per long rest", level, r, pb)
		}
		a, ok := actionNamed(sh, "Fury of the Small")
		if !ok || a.ResourceID != "goblin_fury_of_the_small" || !strings.Contains(a.Description, fmt.Sprintf("extra %d damage", pb)) {
			t.Errorf("level %d: Fury of the Small = %+v, want %d extra damage", level, a, pb)
		}
	}
}
