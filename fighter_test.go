package character

// fighter_test.go - tests for the Fighter and the Fighting Style feats in content/classes.
// Skips itself if the packs aren't there.

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// fighterAt is a level N human fighter with Str 16 (+3), Dex 14 (+2) and Con 14 (+2).
func fighterAt(level int, sel ...Selection) Character {
	return Character{
		ID: "f", Name: "Fighter", RaceID: "human",
		Abilities:  map[Ability]AbilityScore{Str: {Score: 16}, Dex: {Score: 14}, Con: {Score: 14}, Int: {Score: 10}, Wis: {Score: 10}, Cha: {Score: 10}},
		Classes:    []ClassLevel{{ClassID: "fighter", Level: level}},
		Selections: sel,
	}
}

func fighterNote(sh Sheet, prefix string) (string, bool) {
	for _, n := range sh.Notes {
		if strings.HasPrefix(n.Note, prefix) {
			return n.Note, true
		}
	}
	return "", false
}

func TestFighterBasics(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, fighterAt(1), lib, Ruleset{})
	noContentWarnings(t, sh)
	if class, err := lib.Class("fighter"); err != nil || class.HitDie != 10 || class.Name != "Fighter" {
		t.Errorf("class = %+v (err %v)", class, err)
	}
	if !sh.Abilities[Str].SaveProf || !sh.Abilities[Con].SaveProf || sh.Abilities[Dex].SaveProf {
		t.Errorf("saves = %+v", sh.Abilities)
	}
	armour := slices.Clone(sh.ArmourProfs)
	slices.Sort(armour)
	if !slices.Equal(armour, []string{"heavy", "light", "medium", "shield"}) || !slices.Contains(sh.WeaponProfs, "martial") {
		t.Errorf("armour %v, weapons %v", sh.ArmourProfs, sh.WeaponProfs)
	}
	if opts, err := ChoiceOptions(fighterAt(1), lib, Ruleset{}, "class:fighter:start", "skills"); err != nil || len(opts) != 9 {
		t.Errorf("skill options = %d (err %v), want 9", len(opts), err)
	}
	for choice, want := range map[[2]string]int{
		{"fighter_fighting_style", "style"}:   1,
		{"fighter_weapon_mastery", "weapons"}: 3,
	} {
		if p, ok := findPending(sh, choice[0], choice[1]); !ok || p.Remaining != want {
			t.Errorf("%v pending = %+v (found %v), want %d", choice, p, ok, want)
		}
	}
	if opts, err := ChoiceOptions(fighterAt(1), lib, Ruleset{}, "fighter_weapon_mastery", "weapons"); err != nil || len(opts) != 38 {
		t.Errorf("mastery options = %d (err %v), want every Simple and Martial weapon (38)", len(opts), err)
	}
}

func TestFighterFightingStyles(t *testing.T) {
	lib := loadElfLib(t)
	opts, err := ChoiceOptions(fighterAt(1), lib, Ruleset{}, "fighter_fighting_style", "style")
	if err != nil || len(opts) != 10 {
		t.Fatalf("styles = %+v (err %v), want 10", opts, err)
	}
	for _, o := range opts {
		if !o.Eligible {
			t.Errorf("a fighter should qualify for %s: %+v", o.OptionID, o)
		}
	}

	style := func(id string) Character {
		return fighterAt(1, Selection{FeatureID: "fighter_fighting_style", ChoiceID: "style", Picked: []string{id}})
	}
	// Defense: +1 AC in armour (Chain Mail is 16), nothing without it.
	c := style("style_defense")
	if sh := mustCompute(t, c, lib, Ruleset{}); sh.AC != 12 {
		t.Errorf("Defense without armour: AC %d, want 12", sh.AC)
	}
	c.Inventory = []InventoryItem{{ItemID: "chain_mail", Quantity: 1, Equipped: true}}
	sh := mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	if sh.AC != 17 {
		t.Errorf("Defense in Chain Mail: AC %d, want 17", sh.AC)
	}
	// Archery: +2 to hit with ranged weapons only.
	c = style("style_archery")
	c.Inventory = []InventoryItem{{ItemID: "longbow", Quantity: 1, Equipped: true}, {ItemID: "longsword", Quantity: 1, Equipped: true}}
	for _, a := range mustCompute(t, c, lib, Ruleset{}).Attacks {
		switch a.Name {
		case "Longbow":
			if a.ToHit != 2+2+2 {
				t.Errorf("Archery longbow to hit %d, want 6", a.ToHit)
			}
		case "Longsword":
			if a.ToHit != 3+2 {
				t.Errorf("Archery shouldn't help the longsword: to hit %d", a.ToHit)
			}
		}
	}
	if sh := mustCompute(t, style("style_blind_fighting"), lib, Ruleset{}); !slices.ContainsFunc(sh.Senses, func(s Sense) bool { return s.Name == "blindsight" && s.Range == 10 }) {
		t.Errorf("Blind Fighting senses = %+v", sh.Senses)
	}
	if _, ok := actionNamed(mustCompute(t, style("style_protection"), lib, Ruleset{}), "Protection"); !ok {
		t.Error("Protection should give a Reaction")
	}
}

func TestFighterSecondWindAndTacticalMind(t *testing.T) {
	lib := loadElfLib(t)
	for level, uses := range map[int]int{1: 2, 3: 2, 4: 3, 9: 3, 10: 4, 20: 4} {
		sh := mustCompute(t, fighterAt(level), lib, Ruleset{})
		r, ok := findResource(sh, "fighter_second_wind")
		if !ok || r.Max != uses || r.Recharge != RechargeLongRest || r.ShortRestRegain != 1 {
			t.Errorf("level %d: Second Wind %+v, want %d uses, one back on a Short Rest", level, r, uses)
		}
		a, ok := actionNamed(sh, "Second Wind")
		if !ok || a.Type != ActionBonusAction || !strings.Contains(a.Description, fmt.Sprintf("1d10 + %d Hit", level)) {
			t.Errorf("level %d: Second Wind = %+v", level, a)
		}
		if shift := strings.Contains(a.Description, "Tactical Shift"); shift != (level >= 5) {
			t.Errorf("level %d: Tactical Shift in Second Wind = %v", level, shift)
		}
		tm, ok := actionNamed(sh, "Tactical Mind")
		if ok != (level >= 2) || (ok && tm.ResourceID != "fighter_second_wind") {
			t.Errorf("level %d: Tactical Mind = %+v (found %v)", level, tm, ok)
		}
	}
}

func TestFighterScaling(t *testing.T) {
	lib := loadElfLib(t)
	for _, tc := range []struct {
		level, surge, indomitable int
		attacks                   string
	}{
		{1, 0, 0, ""}, {2, 1, 0, ""}, {5, 1, 0, "twice"}, {9, 1, 1, "twice"}, {11, 1, 1, "three times"},
		{13, 1, 2, "three times"}, {17, 2, 3, "three times"}, {20, 2, 3, "four times"},
	} {
		sh := mustCompute(t, fighterAt(tc.level), lib, Ruleset{})
		noContentWarnings(t, sh)
		if r, ok := findResource(sh, "fighter_action_surge"); ok != (tc.surge > 0) || r.Max != tc.surge || (ok && r.Recharge != RechargeShortRest) {
			t.Errorf("level %d: Action Surge %+v (found %v), want %d", tc.level, r, ok, tc.surge)
		}
		if r, ok := findResource(sh, "fighter_indomitable"); ok != (tc.indomitable > 0) || r.Max != tc.indomitable {
			t.Errorf("level %d: Indomitable %+v (found %v), want %d", tc.level, r, ok, tc.indomitable)
		}
		if a, ok := actionNamed(sh, "Indomitable"); ok && !strings.Contains(a.Description, fmt.Sprintf("+%d bonus", tc.level)) {
			t.Errorf("level %d: Indomitable = %q, want a +%d bonus", tc.level, a.Description, tc.level)
		}
		note, ok := fighterNote(sh, "Extra Attack:")
		if tc.attacks == "" && ok || tc.attacks != "" && !strings.Contains(note, tc.attacks) {
			t.Errorf("level %d: extra attack note %q, want %q", tc.level, note, tc.attacks)
		}
		n := 0
		for _, x := range sh.Notes {
			if strings.HasPrefix(x.Note, "Extra Attack:") {
				n++
			}
		}
		if n > 1 {
			t.Errorf("level %d: %d Extra Attack notes, want one", tc.level, n)
		}
		if _, ok := fighterNote(sh, "Tactical Master:"); ok != (tc.level >= 9) {
			t.Errorf("level %d: Tactical Master note = %v", tc.level, ok)
		}
		if _, ok := barbNote(sh, TargetAttackAll, ModAdvantage); ok != (tc.level >= 13) {
			t.Errorf("level %d: Studied Attacks = %v", tc.level, ok)
		}
	}
}

func TestFighterWeaponMasteryGrows(t *testing.T) {
	lib := loadElfLib(t)
	for level, extra := range map[int][]string{3: nil, 4: {"fighter_weapon_mastery_4"}, 10: {"fighter_weapon_mastery_4", "fighter_weapon_mastery_10"},
		16: {"fighter_weapon_mastery_4", "fighter_weapon_mastery_10", "fighter_weapon_mastery_16"}} {
		sh := mustCompute(t, fighterAt(level), lib, Ruleset{})
		total := 0
		for _, id := range append([]string{"fighter_weapon_mastery"}, extra...) {
			if p, ok := findPending(sh, id, "weapons"); ok {
				total += p.Remaining
			}
		}
		if want := 3 + len(extra); total != want {
			t.Errorf("level %d: %d weapon masteries to pick, want %d", level, total, want)
		}
	}
}

func TestFighterFeatChoices(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, fighterAt(20), lib, Ruleset{})
	for _, id := range []string{"fighter_asi_4", "fighter_asi_6", "fighter_asi_8", "fighter_asi_12", "fighter_asi_14", "fighter_asi_16", "fighter_epic_boon"} {
		if p, ok := findPending(sh, id, "feat"); !ok || p.Remaining != 1 {
			t.Errorf("%s pending = %+v (found %v)", id, p, ok)
		}
	}
}

// Fighting Style feats come only through a Fighting Style feature, never the general feat list.
func TestFightingStylesAreNotGeneralFeats(t *testing.T) {
	lib := loadElfLib(t)
	for _, c := range []Character{fighterAt(4), rogueAt(4, "")} {
		feature := c.Classes[0].ClassID + "_asi_4"
		opts, err := ChoiceOptions(c, lib, Ruleset{}, feature, "feat")
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range opts {
			if strings.HasPrefix(o.OptionID, "style_") {
				t.Errorf("%s offers the Fighting Style %s as a feat", feature, o.OptionID)
			}
		}
	}
	// A Champion's second style still comes from the same pool, minus the one already taken.
	c := fighterSub(7, "champion", Selection{FeatureID: "fighter_fighting_style", ChoiceID: "style", Picked: []string{"style_dueling"}})
	opts, err := ChoiceOptions(c, lib, Ruleset{}, "champion_additional_fighting_style", "style")
	if err != nil || len(opts) != 10 {
		t.Fatalf("second style options = %d (err %v), want 10", len(opts), err)
	}
	for _, o := range opts {
		if o.Eligible == (o.OptionID == "style_dueling") {
			t.Errorf("%+v: only Dueling, already taken, should be ineligible", o)
		}
	}
}

func TestDualWielder(t *testing.T) {
	lib := loadElfLib(t)
	for level, eligible := range map[int]bool{3: false, 4: true} {
		// The pool can be asked about before the feature is reached, which is how the level 3 case works.
		opts, err := ChoiceOptions(fighterAt(level), lib, Ruleset{}, "fighter_asi_4", "feat")
		if err != nil {
			t.Fatal(err)
		}
		i := slices.IndexFunc(opts, func(o EligibleOption) bool { return o.OptionID == "dual_wielder" })
		if i < 0 || opts[i].Eligible != eligible {
			t.Errorf("level %d: dual wielder option = %+v, want eligible %v", level, opts, eligible)
		} else if !eligible && opts[i].Reason != "Level 4+, Strength or Dexterity 13+" {
			t.Errorf("level %d: reason %q", level, opts[i].Reason)
		}
	}

	c := fighterAt(4,
		Selection{FeatureID: "fighter_asi_4", ChoiceID: "feat", Picked: []string{"dual_wielder"}},
		Selection{FeatureID: "dual_wielder", ChoiceID: "score", Picked: []string{"dex"}})
	sh := mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	if sh.Abilities[Dex].Score != 15 || sh.Abilities[Str].Score != 16 {
		t.Errorf("Dex %d, Str %d; want 15 and 16", sh.Abilities[Dex].Score, sh.Abilities[Str].Score)
	}
	if a, ok := actionNamed(sh, "Dual Wielder: Extra Attack"); !ok || a.Type != ActionBonusAction {
		t.Errorf("extra attack = %+v", a)
	}
	if _, ok := fighterNote(sh, "Quick Draw:"); !ok {
		t.Error("no Quick Draw note")
	}
	if p, ok := findPending(mustCompute(t, fighterAt(4, c.Selections[0]), lib, Ruleset{}), "dual_wielder", "score"); !ok || p.Remaining != 1 {
		t.Errorf("score pending = %+v (found %v)", p, ok)
	}
}
