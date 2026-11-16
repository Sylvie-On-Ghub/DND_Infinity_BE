package character

// crossbow_expert_test.go - the Crossbow Expert feat: two checked prerequisites, a fixed Dex increase
// and reminder notes for the crossbow rules the engine doesn't model.

import (
	"slices"
	"strings"
	"testing"
)

func TestCrossbowExpertEligibility(t *testing.T) {
	lib := loadElfLib(t)
	eligible := func(c Character) EligibleOption {
		t.Helper()
		opts, err := ChoiceOptions(c, lib, Ruleset{}, "fighter_asi_4", "feat")
		if err != nil {
			t.Fatal(err)
		}
		i := slices.IndexFunc(opts, func(o EligibleOption) bool { return o.OptionID == "crossbow_expert" })
		if i < 0 {
			t.Fatalf("crossbow_expert not offered: %+v", opts)
		}
		return opts[i]
	}

	if o := eligible(fighterAt(4)); !o.Eligible {
		t.Errorf("level 4 with Dex 14 should qualify: %+v", o)
	}
	if o := eligible(fighterAt(3)); o.Eligible || o.Reason != "Level 4+" {
		t.Errorf("level 3 = %+v, want ineligible for Level 4+", o)
	}
	lowDex := fighterAt(4)
	lowDex.Abilities[Dex] = AbilityScore{Score: 12}
	if o := eligible(lowDex); o.Eligible || o.Reason != "Dexterity 13+" {
		t.Errorf("Dex 12 = %+v, want ineligible for Dexterity 13+", o)
	}

	// Taking it anyway is allowed, with a warning. Prerequisites are checked after the feat's own
	// +1 Dex, so Dex 12 would read as 13 here; Dex 11 stays short.
	lowDex.Abilities[Dex] = AbilityScore{Score: 11}
	lowDex.Selections = []Selection{{FeatureID: "fighter_asi_4", ChoiceID: "feat", Picked: []string{"crossbow_expert"}}}
	if sh := mustCompute(t, lowDex, lib, Ruleset{}); !hasWarning(sh, WarnPrereqUnmet) {
		t.Errorf("Dex 11 crossbow expert should warn prereq_unmet: %+v", sh.Warnings)
	}
}

func TestCrossbowExpert(t *testing.T) {
	lib := loadElfLib(t)
	c := fighterAt(4, Selection{FeatureID: "fighter_asi_4", ChoiceID: "feat", Picked: []string{"crossbow_expert"}})
	sh := mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	if sh.Abilities[Dex].Score != 15 {
		t.Errorf("Dex %d, want 15", sh.Abilities[Dex].Score)
	}
	for _, p := range sh.PendingChoices {
		if p.FeatureID == "crossbow_expert" {
			t.Errorf("crossbow expert has no choice to make: %+v", p)
		}
	}
	notes := 0
	for _, n := range sh.Notes {
		if n.Kind == ModNote && strings.HasPrefix(n.Note, "Crossbow Expert:") {
			notes++
		}
	}
	if notes != 3 {
		t.Errorf("%d Crossbow Expert notes, want 3: %+v", notes, sh.Notes)
	}
}
