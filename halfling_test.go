package character

// halfling_test.go - tests for the Halfling in content/core-races. Skips itself if the packs aren't there.

import (
	"slices"
	"strings"
	"testing"
)

func TestHalflingTraits(t *testing.T) {
	lib := loadElfLib(t)
	c := Character{
		ID: "h", RaceID: "halfling",
		Abilities: map[Ability]AbilityScore{Dex: {Score: 16}},
		Classes:   []ClassLevel{{ClassID: "fighter", Level: 1}},
	}
	sh := mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	if sh.Speeds[SpeedWalk] != 30 || darkvision(sh) != 0 {
		t.Errorf("speed %d, darkvision %d", sh.Speeds[SpeedWalk], darkvision(sh))
	}
	if !slices.Equal(sh.Languages, []string{"common", "halfling"}) {
		t.Errorf("languages = %v", sh.Languages)
	}
	if sh.Abilities[Dex].Score != 16 {
		t.Errorf("a race shouldn't change ability scores: Dex %d", sh.Abilities[Dex].Score)
	}
	if n, ok := barbNote(sh, TargetSaveAll, ModAdvantage); !ok || !strings.Contains(n.Condition, "Frightened") {
		t.Errorf("Brave = %+v (found %v)", n, ok)
	}
	for _, target := range []string{TargetCheckAll, TargetSaveAll, "attack.all"} {
		found := false
		for _, n := range sh.Notes {
			found = found || (n.Target == target && n.Kind == ModNote && strings.HasPrefix(n.Note, "Luck:"))
		}
		if !found {
			t.Errorf("Luck: no note on %s in %+v", target, sh.Notes)
		}
	}
	for _, prefix := range []string{"Halfling Nimbleness:", "Naturally Stealthy:"} {
		found := false
		for _, n := range sh.Notes {
			found = found || strings.HasPrefix(n.Note, prefix)
		}
		if !found {
			t.Errorf("no %s note in %+v", prefix, sh.Notes)
		}
	}
}
