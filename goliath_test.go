package character

// goliath_test.go - tests for the Goliath in content/core-races. Skips itself if the packs aren't there.

import (
	"slices"
	"testing"
)

func goliathChar(level int, ancestry string) Character {
	c := Character{
		ID: "g", RaceID: "goliath",
		Abilities: map[Ability]AbilityScore{Str: {Score: 15}},
		Classes:   []ClassLevel{{ClassID: "fighter", Level: level}},
	}
	if ancestry != "" {
		c.Selections = []Selection{{FeatureID: "goliath", ChoiceID: "giant_ancestry", Picked: []string{ancestry}}}
	}
	return c
}

func TestGoliathTraits(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, goliathChar(1, ""), lib, Ruleset{})
	noContentWarnings(t, sh)
	if sh.Speeds[SpeedWalk] != 35 || darkvision(sh) != 0 {
		t.Errorf("speed %d, darkvision %d", sh.Speeds[SpeedWalk], darkvision(sh))
	}
	if !slices.Equal(sh.Languages, []string{"common"}) {
		t.Errorf("languages = %v", sh.Languages)
	}
	if sh.Abilities[Str].Score != 15 {
		t.Errorf("a race shouldn't change ability scores: Str %d", sh.Abilities[Str].Score)
	}
	if p, ok := findPending(sh, "goliath", "giant_ancestry"); !ok || p.Remaining != 1 {
		t.Errorf("ancestry pending = %+v (found %v)", p, ok)
	}
	if opts, err := ChoiceOptions(goliathChar(1, ""), lib, Ruleset{}, "goliath", "giant_ancestry"); err != nil || len(opts) != 6 {
		t.Errorf("ancestries = %+v (err %v), want 6", opts, err)
	}
	// Powerful Build doubles the 15-per-Strength-point capacity.
	if sh.Carry.Capacity != 15*15*2 {
		t.Errorf("carry capacity %d, want %d", sh.Carry.Capacity, 15*15*2)
	}
	if n, ok := barbNote(sh, TargetCheckAll, ModAdvantage); !ok || n.Condition == "" {
		t.Errorf("Powerful Build grapple advantage = %+v (found %v)", n, ok)
	}
	if _, ok := actionNamed(sh, "Large Form"); ok {
		t.Error("Large Form shouldn't be available before level 5")
	}
}

func TestGoliathAncestries(t *testing.T) {
	lib := loadElfLib(t)
	for ancestry, want := range map[string]struct{ name, typ string }{
		"cloud": {"Cloud's Jaunt", "bonus_action"}, "fire": {"Fire's Burn", "other"},
		"frost": {"Frost's Chill", "other"}, "hill": {"Hill's Tumble", "other"},
		"stone": {"Stone's Endurance", "reaction"}, "storm": {"Storm's Thunder", "reaction"},
	} {
		for _, level := range []int{1, 4, 5, 9, 13, 17, 20} {
			sh := mustCompute(t, goliathChar(level, ancestry), lib, Ruleset{})
			noContentWarnings(t, sh)
			id := "goliath_" + ancestry
			if r, ok := findResource(sh, id); !ok || r.Max != sh.ProficiencyBonus || r.Recharge != "long_rest" {
				t.Errorf("%s level %d: uses %+v, want %d per long rest", ancestry, level, r, sh.ProficiencyBonus)
			}
			if a, ok := actionNamed(sh, want.name); !ok || string(a.Type) != want.typ || a.ResourceID != id {
				t.Errorf("%s level %d: %s = %+v", ancestry, level, want.name, a)
			}
		}
	}
}

func TestGoliathLargeForm(t *testing.T) {
	lib := loadElfLib(t)
	for _, level := range []int{5, 11, 20} {
		sh := mustCompute(t, goliathChar(level, "stone"), lib, Ruleset{})
		if r, ok := findResource(sh, "goliath_large_form"); !ok || r.Max != 1 || r.Recharge != "long_rest" {
			t.Errorf("level %d: Large Form uses %+v", level, r)
		}
		if a, ok := actionNamed(sh, "Large Form"); !ok || a.Type != "bonus_action" || a.ResourceID != "goliath_large_form" {
			t.Errorf("level %d: Large Form = %+v", level, a)
		}
		if sh.Speeds[SpeedWalk] != 35 {
			t.Errorf("level %d: Large Form's extra speed only applies while it's active, got %d", level, sh.Speeds[SpeedWalk])
		}
	}
}
