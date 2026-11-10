package character

// possumkin_test.go - tests for the Possumkin in content/homebrew, layered on content/example and
// content/classes. Skips itself if the packs aren't there.

import (
	"slices"
	"strings"
	"testing"
)

func possumkinChar(sel ...Selection) Character {
	return Character{
		ID: "p", RaceID: "possumkin",
		Abilities:  map[Ability]AbilityScore{Str: {Score: 16}, Cha: {Score: 12}},
		Classes:    []ClassLevel{{ClassID: "rogue", Level: 1, RolledHP: []int{8}}},
		Selections: sel,
	}
}

var pickPerception = Selection{FeatureID: "possumkin_scavenger", ChoiceID: "skill", Picked: []string{"perception"}}

func TestPossumkinSpeedsAndSenses(t *testing.T) {
	sh := mustCompute(t, possumkinChar(pickPerception), loadSharkinLib(t), Ruleset{})
	noContentWarnings(t, sh)
	if sh.Speeds[SpeedWalk] != 25 || sh.Speeds[SpeedClimb] != 20 {
		t.Errorf("speeds = %v, want walk 25 and climb 20", sh.Speeds)
	}
	if !slices.Contains(sh.Senses, Sense{Name: "darkvision", Range: 60}) {
		t.Errorf("senses = %+v, want darkvision 60", sh.Senses)
	}
}

func TestPossumkinScavenger(t *testing.T) {
	lib := loadSharkinLib(t)
	if _, ok := findPending(mustCompute(t, possumkinChar(), lib, Ruleset{}), "possumkin_scavenger", "skill"); !ok {
		t.Error("Scavenger should ask for a skill")
	}
	sh := mustCompute(t, possumkinChar(pickPerception), lib, Ruleset{})
	if _, ok := findPending(sh, "possumkin_scavenger", "skill"); ok || sh.Skills["perception"].Level != ProfTrained {
		t.Errorf("perception = %+v, want trained and nothing pending", sh.Skills["perception"])
	}
	bad := Selection{FeatureID: "possumkin_scavenger", ChoiceID: "skill", Picked: []string{"arcana"}}
	if _, ok := findWarning(mustCompute(t, possumkinChar(bad), lib, Ruleset{}), WarnNotInPool); !ok {
		t.Error("Scavenger only offers Sleight of Hand or Perception")
	}
}

func TestPossumkinResilientImmuneSystem(t *testing.T) {
	sh := mustCompute(t, possumkinChar(pickPerception), loadSharkinLib(t), Ruleset{})
	for _, against := range []string{"disease", "poison"} {
		if !slices.ContainsFunc(sh.Notes, func(m Modifier) bool {
			return m.Kind == ModAdvantage && strings.HasPrefix(m.Target, "save.") && strings.Contains(m.Condition, against)
		}) {
			t.Errorf("want advantage on saves against %s: %+v", against, sh.Notes)
		}
	}
}

func TestPossumkinPlayPossumAndBite(t *testing.T) {
	sh := mustCompute(t, possumkinChar(pickPerception), loadSharkinLib(t), Ruleset{})
	if !slices.ContainsFunc(sh.Actions, func(a Action) bool { return a.Name == "Play Possum" && a.Type == ActionReaction }) {
		t.Error("Play Possum reaction missing")
	}
	if r, ok := findResource(sh, "possumkin_play_possum"); !ok || r.Max != 1 || r.Recharge != RechargeShortRest {
		t.Errorf("Play Possum uses = %+v (found %v), want 1 per short rest", r, ok)
	}
	i := slices.IndexFunc(sh.Attacks, func(a AttackView) bool { return a.Name == "Bite" })
	if i < 0 {
		t.Fatalf("no Bite attack: %+v", sh.Attacks)
	}
	if a := sh.Attacks[i]; !a.Proficient || a.ToHit != 5 || a.DamageDice != "1d4" || a.DamageBonus != 3 || a.DamageType != "piercing" {
		t.Errorf("bite = %+v, want proficient, +5 and 1d4+3 piercing", a)
	}
	if !slices.ContainsFunc(sh.Features, func(f FeatureView) bool { return f.FeatureID == "possumkin_strong_tail" }) {
		t.Error("Strong Tail missing")
	}
}
