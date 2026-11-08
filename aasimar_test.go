package character

// aasimar_test.go - tests for the Aasimar in content/core-races. Skips itself if the packs aren't there.

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func aasimarChar(level int) Character {
	return Character{
		ID: "a", RaceID: "aasimar",
		Abilities: map[Ability]AbilityScore{Cha: {Score: 16}},
		Classes:   []ClassLevel{{ClassID: "fighter", Level: level}},
	}
}

func TestAasimarTraits(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, aasimarChar(1), lib, Ruleset{})
	noContentWarnings(t, sh)
	if sh.Speeds[SpeedWalk] != 30 || darkvision(sh) != 60 {
		t.Errorf("speed %d, darkvision %d", sh.Speeds[SpeedWalk], darkvision(sh))
	}
	if sh.Abilities[Cha].Score != 16 {
		t.Errorf("a race shouldn't change ability scores: Cha %d", sh.Abilities[Cha].Score)
	}
	res := slices.Clone(sh.Resistances)
	slices.Sort(res)
	if !slices.Equal(res, []DamageType{"necrotic", "radiant"}) {
		t.Errorf("resistances = %v", sh.Resistances)
	}
	if got := innateSpells(sh); !slices.Equal(got, []string{"light"}) {
		t.Errorf("spells %v", got)
	}
	for _, s := range sh.Spellcasting {
		if s.Kind == SourceFeature && (s.Ability != Cha || s.SaveDC != 8+2+3) {
			t.Errorf("Light Bearer source %s has ability %s, DC %d", s.ID, s.Ability, s.SaveDC)
		}
	}
	if _, ok := actionNamed(sh, "Celestial Revelation: Heavenly Wings"); ok {
		t.Error("Celestial Revelation shouldn't be available before level 3")
	}
}

func TestAasimarScaling(t *testing.T) {
	lib := loadElfLib(t)
	for _, level := range []int{1, 3, 4, 5, 9, 13, 17, 20} {
		sh := mustCompute(t, aasimarChar(level), lib, Ruleset{})
		pb := sh.ProficiencyBonus
		if r, ok := findResource(sh, "aasimar_healing_hands"); !ok || r.Max != 1 || r.Recharge != "long_rest" {
			t.Errorf("level %d: Healing Hands uses %+v", level, r)
		}
		if a, ok := actionNamed(sh, "Healing Hands"); !ok || a.ResourceID != "aasimar_healing_hands" || !strings.Contains(a.Description, fmt.Sprintf("%dd4", pb)) {
			t.Errorf("level %d: Healing Hands = %+v, want %dd4", level, a, pb)
		}
		if level < 3 {
			continue
		}
		if r, ok := findResource(sh, "aasimar_celestial_revelation"); !ok || r.Max != 1 || r.Recharge != "long_rest" {
			t.Errorf("level %d: Celestial Revelation uses %+v", level, r)
		}
		for _, form := range []string{"Heavenly Wings", "Inner Radiance", "Necrotic Shroud"} {
			a, ok := actionNamed(sh, "Celestial Revelation: "+form)
			if !ok || a.ResourceID != "aasimar_celestial_revelation" || !strings.Contains(a.Description, fmt.Sprintf("extra %d ", pb)) {
				t.Errorf("level %d: %s = %+v, want extra %d damage", level, form, a, pb)
			}
		}
	}
}
