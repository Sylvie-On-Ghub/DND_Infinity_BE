package character

// sharkin_test.go - tests for the Sharkin in content/homebrew, layered on content/example and
// content/classes. Skips itself if the packs aren't there.

import (
	"os"
	"testing"
)

func loadSharkinLib(t *testing.T) *ContentLibrary {
	t.Helper()
	loadExample(t)
	if _, err := os.Stat("content/homebrew"); err != nil {
		t.Skip("content/homebrew not found")
	}
	lib, err := LoadDirs(LoadOptions{Strict: true}, "content/example", "content/classes", "content/homebrew")
	if err != nil {
		if lib != nil {
			dumpIssues(t, lib)
		}
		t.Fatalf("the packs failed to load together: %v", err)
	}
	return lib
}

func sharkinChar(level int, sel ...Selection) Character {
	return Character{
		ID: "s", RaceID: "sharkin",
		Abilities:  map[Ability]AbilityScore{Str: {Score: 16}, Int: {Score: 12}, Wis: {Score: 14}},
		Classes:    []ClassLevel{{ClassID: "rogue", Level: level}},
		Selections: sel,
	}
}

var pickHammerhead = Selection{FeatureID: "sharkin", ChoiceID: "subrace", Picked: []string{"hammerhead"}}

func TestSharkinAsksForASubrace(t *testing.T) {
	lib := loadSharkinLib(t)
	sh := mustCompute(t, sharkinChar(1), lib, Ruleset{})
	noContentWarnings(t, sh)

	if _, ok := findPending(sh, "sharkin", "subrace"); !ok {
		t.Errorf("the subrace should be pending: %+v", sh.PendingChoices)
	}
	if sh.Speeds[SpeedWalk] != 30 {
		t.Errorf("walk speed = %d, want 30", sh.Speeds[SpeedWalk])
	}
	if got := sh.Skills["perception"].Passive; got != 12 {
		t.Errorf("passive Perception = %d, want 12 before a subrace is chosen", got)
	}
	for _, a := range sh.Attacks {
		if a.Name == "Vicious Bite" {
			t.Error("the bite comes from the subrace, so it shouldn't appear yet")
		}
	}
}

func TestSharkinHammerheadSenses(t *testing.T) {
	lib := loadSharkinLib(t)
	sh := mustCompute(t, sharkinChar(1, pickHammerhead), lib, Ruleset{})
	noContentWarnings(t, sh)

	if _, ok := findPending(sh, "sharkin", "subrace"); ok {
		t.Error("the subrace is chosen, so it shouldn't be pending")
	}

	per, inv := sh.Skills["perception"], sh.Skills["investigation"]
	if inv.Level != "trained" || inv.Bonus != 3 {
		t.Errorf("Investigation = %+v, want trained with +3", inv)
	}
	if !per.Advantage || !inv.Advantage {
		t.Errorf("Perception advantage %v, Investigation advantage %v; want both", per.Advantage, inv.Advantage)
	}
	// Passive: 10 + bonus, +5 for advantage, and +2 from Lateral Vision on Perception only.
	if per.Passive != 19 {
		t.Errorf("passive Perception = %d, want 19", per.Passive)
	}
	if inv.Passive != 18 {
		t.Errorf("passive Investigation = %d, want 18", inv.Passive)
	}
}

func TestSharkinViciousBiteScales(t *testing.T) {
	lib := loadSharkinLib(t)
	for _, tc := range []struct {
		level int
		dice  string
		toHit int
	}{
		{1, "1d6", 5}, {4, "1d6", 5}, {5, "2d6", 6}, {9, "2d6", 7}, {10, "3d6", 7}, {19, "3d6", 9}, {20, "4d6", 9},
	} {
		sh := mustCompute(t, sharkinChar(tc.level, pickHammerhead), lib, Ruleset{})
		bite := taggedAttack(t, sh, "Vicious Bite")
		if bite.DamageDice != tc.dice || bite.DamageType != "piercing" {
			t.Errorf("level %d: bite = %s %s, want %s piercing", tc.level, bite.DamageDice, bite.DamageType, tc.dice)
		}
		if bite.Ability != Str || !bite.Proficient || bite.ToHit != tc.toHit || bite.RangeNormal != 5 {
			t.Errorf("level %d: bite = %+v, want proficient Str melee at %+d to hit", tc.level, bite, tc.toHit)
		}
	}
}
