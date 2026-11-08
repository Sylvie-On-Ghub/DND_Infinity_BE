package character

// fighter_subclass_test.go - tests for the Champion and Battle Master in content/classes.
// Skips itself if the packs aren't there.

import (
	"strings"
	"testing"
)

func fighterSub(level int, sub string, sel ...Selection) Character {
	c := fighterAt(level, sel...)
	c.Classes[0].SubclassID = sub
	return c
}

func TestChampion(t *testing.T) {
	lib := loadElfLib(t)
	for _, tc := range []struct {
		level int
		crit  string
	}{{3, "19 or 20"}, {14, "19 or 20"}, {15, "18, 19 or 20"}, {20, "18, 19 or 20"}} {
		sh := mustCompute(t, fighterSub(tc.level, "champion"), lib, Ruleset{})
		noContentWarnings(t, sh)
		n := 0
		for _, x := range sh.Notes {
			if strings.Contains(x.Note, "Critical: weapon") {
				n++
				if !strings.Contains(x.Note, tc.crit) {
					t.Errorf("level %d: crit note %q, want %s", tc.level, x.Note, tc.crit)
				}
			}
		}
		if n != 1 {
			t.Errorf("level %d: %d critical-range notes, want one", tc.level, n)
		}
		for _, target := range []string{TargetInitiative, "skill.athletics"} {
			if _, ok := barbNote(sh, target, ModAdvantage); !ok {
				t.Errorf("level %d: Remarkable Athlete: no Advantage on %s", tc.level, target)
			}
		}
		if _, ok := barbNote(sh, TargetDeathSave, ModAdvantage); ok != (tc.level >= 18) {
			t.Errorf("level %d: Survivor death-save Advantage = %v", tc.level, ok)
		}
		if _, ok := fighterNote(sh, "Heroic Warrior:"); ok != (tc.level >= 10) {
			t.Errorf("level %d: Heroic Warrior = %v", tc.level, ok)
		}
	}
	if _, ok := barbNote(mustCompute(t, fighterSub(2, ""), lib, Ruleset{}), TargetInitiative, ModAdvantage); ok {
		t.Error("a level 2 fighter has no subclass features yet")
	}
}

func TestChampionAdditionalFightingStyle(t *testing.T) {
	lib := loadElfLib(t)
	if p, ok := findPending(mustCompute(t, fighterSub(6, "champion"), lib, Ruleset{}), "champion_additional_fighting_style", "style"); ok {
		t.Errorf("no second style before level 7: %+v", p)
	}
	c := fighterSub(7, "champion",
		Selection{FeatureID: "fighter_fighting_style", ChoiceID: "style", Picked: []string{"style_defense"}},
		Selection{FeatureID: "champion_additional_fighting_style", ChoiceID: "style", Picked: []string{"style_archery"}})
	c.Inventory = []InventoryItem{{ItemID: "chain_mail", Quantity: 1, Equipped: true}, {ItemID: "longbow", Quantity: 1, Equipped: true}}
	sh := mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	if sh.AC != 17 {
		t.Errorf("Defense AC = %d, want 17", sh.AC)
	}
	for _, a := range sh.Attacks {
		if a.Name == "Longbow" && a.ToHit != 2+3+2 {
			t.Errorf("Archery longbow to hit %d, want 7", a.ToHit)
		}
	}
	if opts, err := ChoiceOptions(c, lib, Ruleset{}, "champion_additional_fighting_style", "style"); err == nil {
		for _, o := range opts {
			if o.OptionID == "style_defense" && o.Eligible {
				t.Error("Defense is already taken, so it shouldn't be offered again")
			}
		}
	} else {
		t.Error(err)
	}
}

// bmManeuvers picks maneuvers for Combat Superiority, by name without the "maneuver_" prefix.
func bmManeuvers(names ...string) Selection {
	picks := make([]string, len(names))
	for i, n := range names {
		picks[i] = "maneuver_" + n
	}
	return Selection{FeatureID: "battle_master_combat_superiority", ChoiceID: "maneuvers", Picked: picks}
}

func TestBattleMasterSuperiorityDice(t *testing.T) {
	lib := loadElfLib(t)
	for _, tc := range []struct {
		level, dice int
		die         string
	}{{3, 4, "d8"}, {6, 4, "d8"}, {7, 5, "d8"}, {10, 5, "d10"}, {15, 6, "d10"}, {18, 6, "d12"}, {20, 6, "d12"}} {
		sh := mustCompute(t, fighterSub(tc.level, "battle_master", bmManeuvers("trip_attack", "parry", "rally")), lib, Ruleset{})
		noContentWarnings(t, sh)
		r, ok := findResource(sh, "battle_master_combat_superiority")
		if !ok || r.Max != tc.dice || r.Recharge != RechargeShortRest || !strings.Contains(r.Name, tc.die) {
			t.Errorf("level %d: dice %+v, want %d %s per Short Rest", tc.level, r, tc.dice, tc.die)
		}
		for name, typ := range map[string]ActionType{"Trip Attack": ActionOther, "Parry": ActionReaction, "Rally": ActionBonusAction} {
			a, ok := actionNamed(sh, name)
			if !ok || a.Type != typ || a.ResourceID != "battle_master_combat_superiority" || !strings.Contains(a.Description, "("+tc.die+")") {
				t.Errorf("level %d: %s = %+v, want a %s %s spending Superiority Dice", tc.level, name, a, typ, tc.die)
			}
		}
		if _, ok := actionNamed(sh, "Riposte"); ok {
			t.Errorf("level %d: Riposte wasn't picked", tc.level)
		}
	}
}

func TestBattleMasterManeuverPicks(t *testing.T) {
	lib := loadElfLib(t)
	opts, err := ChoiceOptions(fighterSub(3, "battle_master"), lib, Ruleset{}, "battle_master_combat_superiority", "maneuvers")
	if err != nil || len(opts) != 25 {
		t.Fatalf("maneuvers = %d (err %v), want 25", len(opts), err)
	}
	for level, want := range map[int]int{3: 3, 7: 5, 10: 7, 15: 9} {
		sh := mustCompute(t, fighterSub(level, "battle_master"), lib, Ruleset{})
		total := 0
		for _, id := range []string{"battle_master_combat_superiority", "battle_master_maneuvers_7", "battle_master_maneuvers_10", "battle_master_maneuvers_15"} {
			if p, ok := findPending(sh, id, "maneuvers"); ok {
				total += p.Remaining
			}
		}
		if total != want {
			t.Errorf("level %d: %d maneuvers to learn, want %d", level, total, want)
		}
	}
	// Five homebrew maneuvers are labelled as such.
	hb := 0
	for _, o := range opts {
		f, err := lib.Feature(o.OptionID)
		if err == nil && strings.HasSuffix(f.Description, "Homebrew maneuver.") {
			hb++
		}
	}
	if hb != 5 {
		t.Errorf("%d homebrew maneuvers, want 5", hb)
	}
}

func TestBattleMasterOtherFeatures(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, fighterSub(3, "battle_master"), lib, Ruleset{})
	for choice, want := range map[string]int{"tool": 17, "skill": 9} {
		opts, err := ChoiceOptions(fighterSub(3, "battle_master"), lib, Ruleset{}, "battle_master_student_of_war", choice)
		if err != nil || len(opts) != want {
			t.Errorf("Student of War %s options = %d (err %v), want %d", choice, len(opts), err, want)
		}
	}
	c := fighterSub(3, "battle_master",
		Selection{FeatureID: "battle_master_student_of_war", ChoiceID: "tool", Picked: []string{"smiths_tools"}},
		Selection{FeatureID: "battle_master_student_of_war", ChoiceID: "skill", Picked: []string{"history"}})
	sh = mustCompute(t, c, lib, Ruleset{})
	if _, ok := sh.Tools["smiths_tools"]; !ok {
		t.Errorf("tools = %v, want smiths_tools", sh.Tools)
	}
	if sh.Skills["history"].Level != ProfTrained {
		t.Errorf("history = %+v, want proficient", sh.Skills["history"])
	}
	if _, ok := actionNamed(sh, "Know Your Enemy"); ok {
		t.Error("Know Your Enemy starts at level 7")
	}
	sh = mustCompute(t, fighterSub(15, "battle_master"), lib, Ruleset{})
	if r, ok := findResource(sh, "battle_master_know_your_enemy"); !ok || r.Max != 1 || r.Recharge != RechargeLongRest {
		t.Errorf("Know Your Enemy %+v", r)
	}
	if _, ok := fighterNote(sh, "Relentless:"); !ok {
		t.Error("level 15: no Relentless note")
	}
}
