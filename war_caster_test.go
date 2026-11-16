package character

// war_caster_test.go - the 2024 War Caster feat: Level 4+ (no spellcasting check), a +1 to Int, Wis
// or Cha, a situational Concentration advantage, Reactive Spell and a Somatic Components note.

import (
	"slices"
	"strings"
	"testing"
)

func TestWarCaster(t *testing.T) {
	lib := loadElfLib(t)
	for level, eligible := range map[int]bool{3: false, 4: true} {
		opts, err := ChoiceOptions(fighterAt(level), lib, Ruleset{}, "fighter_asi_4", "feat")
		if err != nil {
			t.Fatal(err)
		}
		i := slices.IndexFunc(opts, func(o EligibleOption) bool { return o.OptionID == "war_caster" })
		if i < 0 || opts[i].Eligible != eligible {
			t.Errorf("level %d: war caster option = %+v, want eligible %v", level, opts, eligible)
		}
	}

	c := fighterAt(4,
		Selection{FeatureID: "fighter_asi_4", ChoiceID: "feat", Picked: []string{"war_caster"}},
		Selection{FeatureID: "war_caster", ChoiceID: "score", Picked: []string{"wis"}})
	sh := mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	if hasWarning(sh, WarnPrereqUnverifiable) || hasWarning(sh, WarnPrereqUnmet) {
		t.Errorf("a level 4 fighter meets every War Caster prerequisite: %+v", sh.Warnings)
	}
	if sh.Abilities[Wis].Score != 11 {
		t.Errorf("Wis %d, want 11", sh.Abilities[Wis].Score)
	}
	if a, ok := actionNamed(sh, "War Caster: Reactive Spell"); !ok || a.Type != ActionReaction {
		t.Errorf("reactive spell = %+v (found %v)", a, ok)
	}
	var concentration, somatic bool
	for _, n := range sh.Notes {
		if n.Target == SaveTarget(Con) && n.Kind == ModAdvantage && n.Condition == "to maintain Concentration" {
			concentration = true
		}
		if strings.HasPrefix(n.Note, "War Caster:") {
			somatic = true
		}
	}
	if !concentration || !somatic {
		t.Errorf("concentration note %v, somatic note %v: %+v", concentration, somatic, sh.Notes)
	}
	if sh.Abilities[Con].SaveBonus != mustCompute(t, fighterAt(4), lib, Ruleset{}).Abilities[Con].SaveBonus {
		t.Error("the Concentration advantage is situational and shouldn't change the Con save")
	}
}
