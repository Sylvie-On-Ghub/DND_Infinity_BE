package character

// gnome_test.go - tests for the Gnome in content/core-races. Skips itself if the packs aren't there.

import (
	"slices"
	"testing"
)

// gnomeChar is a rogue gnome of the lineage with the spellcasting ability chosen.
func gnomeChar(lineage, ability string, level int) Character {
	c := Character{
		ID: "g", RaceID: "gnome",
		Abilities: map[Ability]AbilityScore{Int: {Score: 16}, Wis: {Score: 14}, Cha: {Score: 12}},
		Classes:   []ClassLevel{{ClassID: "rogue", Level: level}},
	}
	if lineage != "" {
		c.Selections = append(c.Selections, Selection{FeatureID: "gnome", ChoiceID: "lineage", Picked: []string{lineage}})
	}
	if ability != "" {
		c.Selections = append(c.Selections, Selection{FeatureID: "gnome_" + lineage, ChoiceID: "spellcasting_ability", Picked: []string{ability}})
	}
	return c
}

func TestGnomeTraits(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, gnomeChar("", "", 1), lib, Ruleset{})
	if sh.Speeds[SpeedWalk] != 30 || darkvision(sh) != 60 {
		t.Errorf("speed %d, darkvision %d", sh.Speeds[SpeedWalk], darkvision(sh))
	}
	if !slices.Equal(sh.Languages, []string{"common", "gnomish"}) {
		t.Errorf("languages = %v", sh.Languages)
	}
	if sh.Abilities[Int].Score != 16 {
		t.Errorf("a race shouldn't change ability scores: Int %d", sh.Abilities[Int].Score)
	}
	for _, a := range []string{"int", "wis", "cha"} {
		if _, ok := barbNote(sh, "save."+a, ModAdvantage); !ok {
			t.Errorf("Gnomish Cunning: no Advantage on %s saves in %+v", a, sh.Notes)
		}
	}
	if _, ok := barbNote(sh, "save.str", ModAdvantage); ok {
		t.Error("Gnomish Cunning doesn't cover Strength saves")
	}
	if p, ok := findPending(sh, "gnome", "lineage"); !ok || p.Remaining != 1 {
		t.Errorf("lineage pending = %+v (found %v)", p, ok)
	}
	if p, ok := findPending(mustCompute(t, gnomeChar("rock", "", 1), lib, Ruleset{}), "gnome_rock", "spellcasting_ability"); !ok || p.Remaining != 1 {
		t.Errorf("spellcasting ability pending = %+v (found %v)", p, ok)
	}
}

func TestForestGnome(t *testing.T) {
	lib := loadElfLib(t)
	for ability, mod := range map[string]int{"int": 3, "wis": 2, "cha": 1} {
		sh := mustCompute(t, gnomeChar("forest", ability, 1), lib, Ruleset{})
		noContentWarnings(t, sh)
		if got := innateSpells(sh); !slices.Equal(got, []string{"minor_illusion", "speak_with_animals"}) {
			t.Errorf("%s: spells %v", ability, got)
		}
		for _, s := range sh.Spellcasting {
			if s.Kind == SourceFeature && (string(s.Ability) != ability || s.SaveDC != 8+2+mod) {
				t.Errorf("%s: source %s has ability %s, DC %d", ability, s.ID, s.Ability, s.SaveDC)
			}
		}
	}
	// Speak with Animals uses equal the Proficiency Bonus.
	for level, uses := range map[int]int{1: 2, 4: 2, 5: 3, 9: 4, 13: 5, 17: 6, 20: 6} {
		sh := mustCompute(t, gnomeChar("forest", "wis", level), lib, Ruleset{})
		found := false
		for _, r := range sh.Resources {
			if r.FeatureID == "feature:gnome_forest_speak_with_animals_wis" {
				found = true
				if r.Max != uses || r.Max != sh.ProficiencyBonus || r.Recharge != RechargeLongRest {
					t.Errorf("level %d: %+v, want %d uses per long rest", level, r, uses)
				}
			}
		}
		if !found {
			t.Errorf("level %d: no Speak with Animals uses in %+v", level, sh.Resources)
		}
	}
}

func TestRockGnome(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, gnomeChar("rock", "int", 1), lib, Ruleset{})
	noContentWarnings(t, sh)
	if got := innateSpells(sh); !slices.Equal(got, []string{"mending", "prestidigitation"}) {
		t.Errorf("spells %v", got)
	}
	for _, s := range sh.Spellcasting {
		if s.Kind == SourceFeature && s.Ability != Int {
			t.Errorf("source %s uses %s, want int", s.ID, s.Ability)
		}
	}
	if a, ok := actionNamed(sh, "Clockwork Device"); !ok || a.Duration != "8 hours" {
		t.Errorf("Clockwork Device = %+v", a)
	}
	if len(sh.Resources) != 0 {
		t.Errorf("rock gnome cantrips need no uses: %+v", sh.Resources)
	}
}
