package character

// elf_test.go - tests for the Elf in content/core-races, layered on content/example and content/classes
// (whose spells it shares). Skips itself if the packs aren't there.

import (
	"os"
	"slices"
	"testing"
)

func loadElfLib(t *testing.T) *ContentLibrary {
	t.Helper()
	loadExample(t)
	for _, dir := range []string{"content/classes", "content/core-races"} {
		if _, err := os.Stat(dir); err != nil {
			t.Skipf("%s not found", dir)
		}
	}
	lib, err := LoadDirs(LoadOptions{Strict: true}, "content/example", "content/classes", "content/core-races")
	if err != nil {
		if lib != nil {
			dumpIssues(t, lib)
		}
		t.Fatalf("the packs failed to load together: %v", err)
	}
	return lib
}

// elfChar is an elf of the lineage with the ability chosen, at the given levels of wizard and rogue.
func elfChar(lineage, ability string, wizard, rogue int, extra ...Selection) Character {
	c := Character{
		ID: "e", RaceID: "elf",
		Abilities: map[Ability]AbilityScore{Int: {Score: 16}, Wis: {Score: 14}, Cha: {Score: 12}},
		Selections: append([]Selection{
			{FeatureID: "elf", ChoiceID: "lineage", Picked: []string{lineage}},
			{FeatureID: "elf_" + lineage, ChoiceID: "spellcasting_ability", Picked: []string{ability}},
			{FeatureID: "elf_keen_senses", ChoiceID: "skill", Picked: []string{"perception"}},
		}, extra...),
	}
	if wizard > 0 {
		c.Classes = append(c.Classes, ClassLevel{ClassID: "wizard", Level: wizard})
	}
	if rogue > 0 {
		c.Classes = append(c.Classes, ClassLevel{ClassID: "rogue", Level: rogue})
	}
	return c
}

// innateSpells lists the spells from the elf's innate sources, cantrips and levelled alike.
func innateSpells(sh Sheet) []string {
	out := []string{}
	for _, s := range sh.Spellcasting {
		if s.Kind != SourceFeature {
			continue
		}
		for _, v := range append(s.Cantrips, s.Spells...) {
			out = append(out, v.ID)
		}
	}
	slices.Sort(out)
	return out
}

func darkvision(sh Sheet) int {
	for _, s := range sh.Senses {
		if s.Name == "darkvision" {
			return s.Range
		}
	}
	return 0
}

func TestElfBaseTraits(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, Character{ID: "e", RaceID: "elf", Abilities: map[Ability]AbilityScore{Int: {Score: 16}}}, lib, Ruleset{})
	noContentWarnings(t, sh)
	if sh.Speeds[SpeedWalk] != 30 || darkvision(sh) != 60 {
		t.Errorf("speed %d, darkvision %d", sh.Speeds[SpeedWalk], darkvision(sh))
	}
	if !slices.Equal(sh.Languages, []string{"common", "elvish"}) {
		t.Errorf("languages = %v", sh.Languages)
	}
	if sh.Abilities[Int].Score != 16 {
		t.Errorf("a race shouldn't change ability scores: Int %d", sh.Abilities[Int].Score)
	}
	for _, want := range [][2]string{{"elf", "lineage"}, {"elf_keen_senses", "skill"}} {
		if _, ok := findPending(sh, want[0], want[1]); !ok {
			t.Errorf("%s/%s should be pending: %+v", want[0], want[1], sh.PendingChoices)
		}
	}
}

func TestElfLineages(t *testing.T) {
	lib := loadElfLib(t)
	for _, tc := range []struct {
		lineage, cantrip, spell3, spell5 string
		speed, dark                      int
	}{
		{"drow", "dancing_lights", "faerie_fire", "darkness", 30, 120},
		{"wood", "druidcraft", "longstrider", "pass_without_trace", 35, 60},
		{"shadowmoor", "starry_wisp", "heroism", "gentle_repose", 30, 120},
		{"high", "prestidigitation", "detect_magic", "misty_step", 30, 60},
		{"lorwyn", "thorn_whip", "command", "silence", 30, 60},
	} {
		t.Run(tc.lineage, func(t *testing.T) {
			var extra []Selection
			if tc.lineage == "high" || tc.lineage == "lorwyn" {
				extra = append(extra, Selection{FeatureID: "elf_" + tc.lineage + "_magic_int", ChoiceID: "cantrip", Picked: []string{tc.cantrip}})
			}
			for _, step := range []struct {
				level int
				want  []string
			}{
				{1, []string{tc.cantrip}},
				{2, []string{tc.cantrip}},
				{3, []string{tc.cantrip, tc.spell3}},
				{4, []string{tc.cantrip, tc.spell3}},
				{5, []string{tc.cantrip, tc.spell3, tc.spell5}},
				{9, []string{tc.cantrip, tc.spell3, tc.spell5}},
			} {
				sh := mustCompute(t, elfChar(tc.lineage, "int", step.level, 0, extra...), lib, Ruleset{})
				noContentWarnings(t, sh)
				slices.Sort(step.want)
				if got := innateSpells(sh); !slices.Equal(got, step.want) {
					t.Errorf("level %d: spells %v, want %v", step.level, got, step.want)
				}
				if sh.Speeds[SpeedWalk] != tc.speed || darkvision(sh) != tc.dark {
					t.Errorf("level %d: speed %d, darkvision %d", step.level, sh.Speeds[SpeedWalk], darkvision(sh))
				}
				for _, p := range sh.PendingChoices {
					if p.FeatureID != "" && p.FeatureID[:3] == "elf" {
						t.Errorf("level %d: nothing of the elf's should be pending: %+v", step.level, p)
					}
				}
			}
		})
	}
}

// Spells unlock at character level, so levels in two classes add up.
func TestElfSpellsUseCharacterLevel(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, elfChar("drow", "int", 2, 1), lib, Ruleset{})
	if got := innateSpells(sh); !slices.Contains(got, "faerie_fire") || slices.Contains(got, "darkness") {
		t.Errorf("wizard 2 / rogue 1: spells %v", got)
	}
	sh = mustCompute(t, elfChar("drow", "int", 3, 2), lib, Ruleset{})
	if got := innateSpells(sh); !slices.Contains(got, "darkness") {
		t.Errorf("wizard 3 / rogue 2: spells %v", got)
	}
}

func TestElfSpellcastingAbilityAndUses(t *testing.T) {
	lib := loadElfLib(t)
	for ability, mod := range map[string]int{"int": 3, "wis": 2, "cha": 1} {
		sh := mustCompute(t, elfChar("drow", ability, 5, 0), lib, Ruleset{})
		sources := 0
		for _, s := range sh.Spellcasting {
			if s.Kind != SourceFeature {
				continue
			}
			sources++
			if string(s.Ability) != ability || s.SaveDC != 8+3+mod { // PB 3 at level 5
				t.Errorf("%s: source %s has ability %s, DC %d", ability, s.ID, s.Ability, s.SaveDC)
			}
		}
		if sources != 3 {
			t.Errorf("%s: %d innate sources, want the cantrip and two spells", ability, sources)
		}
		// Each levelled spell is cast once per long rest, separately.
		for _, id := range []string{"elf_drow_faerie_fire_" + ability, "elf_drow_darkness_" + ability} {
			found := false
			for _, r := range sh.Resources {
				if r.FeatureID == "feature:"+id && r.Max == 1 && r.Recharge == RechargeLongRest {
					found = true
				}
			}
			if !found {
				t.Errorf("%s has no use of its own in %+v", id, sh.Resources)
			}
		}
	}
}

func TestElfSwappableCantrips(t *testing.T) {
	lib := loadElfLib(t)
	c := elfChar("high", "int", 1, 0)
	sh := mustCompute(t, c, lib, Ruleset{})
	if _, ok := findPending(sh, "elf_high_magic_int", "cantrip"); !ok {
		t.Fatalf("the cantrip should be pending: %+v", sh.PendingChoices)
	}
	opts, err := ChoiceOptions(c, lib, Ruleset{}, "elf_high_magic_int", "cantrip")
	if err != nil {
		t.Fatal(err)
	}
	if len(opts) != 4 {
		t.Errorf("wizard cantrip options = %+v", opts)
	}
	if err := UpsertSelection(&c, Selection{FeatureID: "elf_high_magic_int", ChoiceID: "cantrip", Picked: []string{"fire_bolt"}}); err != nil {
		t.Fatal(err)
	}
	if got := innateSpells(mustCompute(t, c, lib, Ruleset{})); !slices.Equal(got, []string{"fire_bolt"}) {
		t.Errorf("spells = %v", got)
	}
	// Swapping replaces the old pick.
	if err := UpsertSelection(&c, Selection{FeatureID: "elf_high_magic_int", ChoiceID: "cantrip", Picked: []string{"prestidigitation"}}); err != nil {
		t.Fatal(err)
	}
	if got := innateSpells(mustCompute(t, c, lib, Ruleset{})); !slices.Equal(got, []string{"prestidigitation"}) {
		t.Errorf("spells after the swap = %v", got)
	}
}

func TestElfFeyAncestryAndKeenSenses(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, elfChar("wood", "wis", 1, 0), lib, Ruleset{})
	if sh.Skills["perception"].Level != ProfTrained {
		t.Errorf("perception = %+v", sh.Skills["perception"])
	}
	// Fey Ancestry is situational, so it's a note rather than a change to any number.
	found := false
	for _, n := range sh.Notes {
		if n.Kind == ModAdvantage && n.Target == "save.all" && n.Condition == "to avoid or end the Charmed condition" {
			found = true
		}
	}
	if !found {
		t.Errorf("Fey Ancestry missing from the notes: %+v", sh.Notes)
	}
}

// The packs the server loads by default, in their default order.
func TestDefaultPacks(t *testing.T) {
	loadElfLib(t)
	dirs, err := FindPacks("content")
	if err != nil {
		t.Fatal(err)
	}
	lib, err := LoadDirs(LoadOptions{Strict: true}, dirs...)
	if err != nil {
		dumpIssues(t, lib)
		t.Fatal(err)
	}
	for _, id := range []string{"rogue", "warlock", "wizard"} {
		if _, err := lib.Class(id); err != nil {
			t.Error(err)
		}
	}
	for _, id := range []string{"aasimar", "dragonborn", "dwarf", "elf", "gnome", "goblin", "goliath", "halfling", "human", "orc", "tiefling"} {
		if _, err := lib.Lookup("races", id); err != nil {
			t.Error(err)
		}
	}
	// core-races loads after classes, so the Tiefling's version of a shared spell wins.
	sp, err := lib.Spell("chill_touch")
	if err != nil {
		t.Fatal(err)
	}
	if sp.Effects[0].Dice != "1d10" {
		t.Errorf("chill_touch does %s, want the 1d10 from core-races", sp.Effects[0].Dice)
	}
}
