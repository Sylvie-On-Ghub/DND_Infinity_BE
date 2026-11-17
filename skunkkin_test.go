package character

// skunkkin_test.go - tests for the Skunkkin in content/homebrew, layered on content/example and
// content/classes. Skips itself if the packs aren't there.

import (
	"slices"
	"testing"
)

func skunkkinChar(level int, abilities map[Ability]AbilityScore) Character {
	hp := []int{8}
	for len(hp) < level {
		hp = append(hp, 5)
	}
	return Character{
		ID: "s", RaceID: "skunkkin",
		Abilities: abilities,
		Classes:   []ClassLevel{{ClassID: "rogue", Level: level, RolledHP: hp}},
	}
}

func skunkkinAbilities(str, dex, con int) map[Ability]AbilityScore {
	return map[Ability]AbilityScore{Str: {Score: str}, Dex: {Score: dex}, Con: {Score: con}, Cha: {Score: 10}}
}

func TestSkunkkinTraits(t *testing.T) {
	lib := loadSharkinLib(t)
	sh := mustCompute(t, skunkkinChar(1, skunkkinAbilities(10, 16, 14)), lib, Ruleset{})
	noContentWarnings(t, sh)
	if sh.Speeds[SpeedWalk] != 30 || sh.Speeds[SpeedBurrow] != 5 {
		t.Errorf("speeds = %v, want walk 30 and burrow 5", sh.Speeds)
	}
	if !slices.Contains(sh.Senses, Sense{Name: "darkvision", Range: 60}) {
		t.Errorf("senses = %+v, want darkvision 60", sh.Senses)
	}
	if sh.Skills["acrobatics"].Level != ProfTrained {
		t.Errorf("acrobatics = %+v, want trained", sh.Skills["acrobatics"])
	}
	if sh.Skills["perception"].Advantage {
		t.Error("Keen Senses is situational: Perception shouldn't have flat Advantage")
	}
	found := false
	for _, n := range sh.Notes {
		if n.Target == SkillTarget("perception") && n.Kind == ModAdvantage && n.Condition == "relying on hearing or smell" {
			found = true
		}
	}
	if !found {
		t.Errorf("no Keen Senses note: %+v", sh.Notes)
	}
}

func TestSkunkkinClawsUseTheBetterAbility(t *testing.T) {
	lib := loadSharkinLib(t)
	for _, tc := range []struct {
		str, dex int
		want     Ability
	}{{16, 10, Str}, {10, 16, Dex}} {
		sh := mustCompute(t, skunkkinChar(1, skunkkinAbilities(tc.str, tc.dex, 10)), lib, Ruleset{})
		a, ok := findAttack(sh, "Claws")
		if !ok {
			t.Fatalf("no Claws attack: %+v", sh.Attacks)
		}
		// +3 from the ability, +2 proficiency.
		if a.Ability != tc.want || a.ToHit != 5 || a.DamageDice != "1d4" || a.DamageBonus != 3 || a.DamageType != "slashing" {
			t.Errorf("Str %d Dex %d: claws = %+v, want %s, +5 to hit, 1d4+3 slashing", tc.str, tc.dex, a, tc.want)
		}
	}
}

func TestSkunkkinNoxiousSprayUses(t *testing.T) {
	lib := loadSharkinLib(t)
	for _, tc := range []struct {
		level, con, want int
	}{
		{1, 14, 4}, // +2 Con, +2 PB
		{1, 8, 2},  // -1 Con +2 PB = 1, raised to the minimum of 2
		{5, 14, 5}, // +2 Con, +3 PB
		{17, 14, 8},
	} {
		sh := mustCompute(t, skunkkinChar(tc.level, skunkkinAbilities(10, 10, tc.con)), lib, Ruleset{})
		r, ok := findResource(sh, "skunkkin_noxious_spray")
		if !ok || r.Max != tc.want || r.Recharge != RechargeLongRest {
			t.Errorf("level %d Con %d: spray = %+v (found %v), want %d per long rest", tc.level, tc.con, r, ok, tc.want)
		}
	}
}

func TestSkunkkinLingeringMusk(t *testing.T) {
	lib := loadSharkinLib(t)
	c := skunkkinChar(1, skunkkinAbilities(10, 10, 10))
	off := mustCompute(t, c, lib, Ruleset{})
	c.ActiveForms = []ActiveForm{{ID: "lingering_musk"}}
	on := mustCompute(t, c, lib, Ruleset{})
	if got, want := on.Skills["persuasion"].Bonus, off.Skills["persuasion"].Bonus-1; got != want {
		t.Errorf("persuasion with Lingering Musk = %d, want %d", got, want)
	}
	if on.Skills["stealth"].Bonus != off.Skills["stealth"].Bonus {
		t.Error("Lingering Musk should only affect Charisma checks")
	}
	if _, ok := findResource(on, "skunkkin_lingering_musk"); ok {
		t.Error("Lingering Musk costs nothing: it shouldn't have a resource")
	}
}
