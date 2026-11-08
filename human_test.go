package character

// human_test.go - tests for the Human in content/core-races. Skips itself if the packs aren't there.

import (
	"slices"
	"testing"
)

func humanChar(sel ...Selection) Character {
	return Character{
		ID: "h", RaceID: "human",
		Abilities:  map[Ability]AbilityScore{Dex: {Score: 12}},
		Classes:    []ClassLevel{{ClassID: "fighter", Level: 1}},
		Selections: sel,
	}
}

func TestHumanTraits(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, humanChar(), lib, Ruleset{})
	noContentWarnings(t, sh)
	if sh.Speeds[SpeedWalk] != 30 || darkvision(sh) != 0 {
		t.Errorf("speed %d, darkvision %d", sh.Speeds[SpeedWalk], darkvision(sh))
	}
	if !slices.Equal(sh.Languages, []string{"common"}) {
		t.Errorf("languages = %v", sh.Languages)
	}
	if sh.Abilities[Dex].Score != 12 {
		t.Errorf("a race shouldn't change ability scores: Dex %d", sh.Abilities[Dex].Score)
	}
	if _, ok := barbNote(sh, "check.all", ModNote); !ok {
		t.Errorf("Resourceful should be a note: %+v", sh.Notes)
	}
	if _, ok := findPending(sh, "human_skillful", "skill"); !ok {
		t.Error("Skillful should ask for a skill")
	}
	if _, ok := findPending(sh, "human_versatile", "feat"); !ok {
		t.Error("Versatile should ask for an origin feat")
	}
	opts, err := ChoiceOptions(humanChar(), lib, Ruleset{}, "human_skillful", "skill")
	if err != nil || len(opts) != 18 {
		t.Errorf("Skillful offers %d skills (err %v), want all 18", len(opts), err)
	}
	opts, err = ChoiceOptions(humanChar(), lib, Ruleset{}, "human_versatile", "feat")
	if err != nil || !slices.ContainsFunc(opts, func(o EligibleOption) bool { return o.OptionID == "skilled" }) {
		t.Errorf("Versatile options = %+v (err %v), want Skilled among them", opts, err)
	}
}

func TestHumanPicksApply(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, humanChar(
		Selection{FeatureID: "human_skillful", ChoiceID: "skill", Picked: []string{"survival"}},
		Selection{FeatureID: "human_versatile", ChoiceID: "feat", Picked: []string{"musician"}},
	), lib, Ruleset{})
	if sh.Skills["survival"].Level != ProfTrained {
		t.Errorf("survival = %+v", sh.Skills["survival"])
	}
	if _, ok := findFeature(sh, "musician"); !ok {
		t.Error("the picked origin feat should be on the sheet")
	}
	if _, ok := findPending(sh, "musician", "instruments"); !ok {
		t.Error("the feat's own choices should follow it")
	}
}
