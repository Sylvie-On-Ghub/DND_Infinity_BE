package character

// rogue_test.go - tests for the Rogue, the Thief and the Arcane Trickster in content/classes.
// Skips itself if the packs aren't there.

import (
	"slices"
	"strings"
	"testing"
)

// rogueAt is a level N human rogue with Dex 16 (+3) and Int 14 (+2), in the subclass from level 3.
func rogueAt(level int, sub string, sel ...Selection) Character {
	cl := ClassLevel{ClassID: "rogue", Level: level}
	if level >= 3 {
		cl.SubclassID = sub
	}
	return Character{
		ID: "r", Name: "Rogue", RaceID: "human",
		Abilities:  map[Ability]AbilityScore{Str: {Score: 10}, Dex: {Score: 16}, Con: {Score: 14}, Int: {Score: 14}, Wis: {Score: 10}, Cha: {Score: 10}},
		Classes:    []ClassLevel{cl},
		Selections: sel,
	}
}

func TestRogueBasics(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, rogueAt(1, ""), lib, Ruleset{})
	noContentWarnings(t, sh)
	if class, err := lib.Class("rogue"); err != nil || class.HitDie != 8 {
		t.Errorf("class = %+v (err %v)", class, err)
	}
	if !sh.Abilities[Dex].SaveProf || !sh.Abilities[Int].SaveProf || sh.Abilities[Wis].SaveProf {
		t.Errorf("saves = %+v", sh.Abilities)
	}
	weapons := slices.Clone(sh.WeaponProfs)
	slices.Sort(weapons)
	if !slices.Equal(weapons, []string{"hand_crossbow", "rapier", "scimitar", "shortsword", "simple", "whip"}) || !slices.Equal(sh.ArmourProfs, []string{"light"}) {
		t.Errorf("weapons %v, armour %v", sh.WeaponProfs, sh.ArmourProfs)
	}
	if _, ok := sh.Tools["thieves_tools"]; !ok {
		t.Errorf("tools = %v", sh.Tools)
	}
	if !slices.Contains(sh.Languages, "thieves_cant") {
		t.Errorf("languages = %v, want Thieves' Cant", sh.Languages)
	}
	for _, p := range []struct {
		feature, choice string
		n               int
	}{{"class:rogue:start", "skills", 4}, {"rogue_expertise_1", "expertise", 2}, {"rogue_thieves_cant", "language", 1}, {"rogue_weapon_mastery", "weapons", 2}} {
		if got, ok := findPending(sh, p.feature, p.choice); !ok || got.Remaining != p.n {
			t.Errorf("%s/%s pending = %+v (found %v), want %d", p.feature, p.choice, got, ok, p.n)
		}
	}
	if opts, _ := ChoiceOptions(rogueAt(1, ""), lib, Ruleset{}, "class:rogue:start", "skills"); len(opts) != 10 {
		t.Errorf("skill options = %d, want 10", len(opts))
	}
	if opts, _ := ChoiceOptions(rogueAt(1, ""), lib, Ruleset{}, "rogue_weapon_mastery", "weapons"); len(opts) != 19 {
		t.Errorf("mastery options = %d, want the 19 weapons a rogue is proficient with", len(opts))
	}
}

func TestRogueSneakAttackAndCunningStrike(t *testing.T) {
	lib := loadElfLib(t)
	for level := 1; level <= 20; level++ {
		sh := mustCompute(t, rogueAt(level, "thief"), lib, Ruleset{})
		noContentWarnings(t, sh)
		want := []string{"", "1d6", "1d6", "2d6", "2d6", "3d6", "3d6", "4d6", "4d6", "5d6", "5d6", "6d6", "6d6", "7d6", "7d6", "8d6", "8d6", "9d6", "9d6", "10d6", "10d6"}[level]
		// Sneak Attack rides on each attack entry; the unarmed strike is always there.
		found := false
		for _, a := range sh.Attacks {
			for _, r := range a.DamageRiders {
				if strings.Contains(r.Condition, "Finesse or Ranged") {
					found = true
					if r.Dice != want {
						t.Errorf("level %d: Sneak Attack %s on %s, want %s", level, r.Dice, a.Name, want)
					}
				}
			}
		}
		if !found {
			t.Errorf("level %d: no Sneak Attack rider on %+v", level, sh.Attacks)
		}
		a, ok := actionNamed(sh, "Cunning Strike")
		switch {
		case level < 5:
			if ok {
				t.Errorf("level %d: Cunning Strike starts at 5", level)
			}
		case !ok:
			t.Errorf("level %d: no Cunning Strike", level)
		default:
			if two := strings.Contains(a.Description, "up to two"); two != (level >= 11) {
				t.Errorf("level %d: two effects = %v", level, two)
			}
			if devious := strings.Contains(a.Description, "Knock Out (6d6)"); devious != (level >= 14) {
				t.Errorf("level %d: Devious Strikes options = %v", level, devious)
			}
			if !strings.Contains(a.Description, "Withdraw (1d6)") {
				t.Errorf("level %d: Cunning Strike = %q", level, a.Description)
			}
		}
	}
}

func TestRogueLaterFeatures(t *testing.T) {
	lib := loadElfLib(t)
	for _, tc := range []struct {
		level   int
		actions []string
		notes   []string
	}{
		{2, []string{"Cunning Action"}, nil},
		{3, []string{"Cunning Action", "Steady Aim"}, nil},
		{5, []string{"Uncanny Dodge", "Cunning Strike"}, nil},
		{7, nil, []string{"Evasion:", "Reliable Talent:"}},
		{18, nil, []string{"Elusive:"}},
		{20, []string{"Stroke of Luck"}, nil},
	} {
		sh := mustCompute(t, rogueAt(tc.level, "thief"), lib, Ruleset{})
		for _, a := range tc.actions {
			if _, ok := actionNamed(sh, a); !ok {
				t.Errorf("level %d: no %s action", tc.level, a)
			}
		}
		for _, n := range tc.notes {
			if _, ok := fighterNote(sh, n); !ok {
				t.Errorf("level %d: no %s note", tc.level, n)
			}
		}
	}
	if _, ok := actionNamed(mustCompute(t, rogueAt(1, ""), lib, Ruleset{}), "Cunning Action"); ok {
		t.Error("Cunning Action starts at level 2")
	}
	// Slippery Mind adds Wisdom and Charisma saves at 15.
	for level, want := range map[int]bool{14: false, 15: true} {
		sh := mustCompute(t, rogueAt(level, "thief"), lib, Ruleset{})
		if sh.Abilities[Wis].SaveProf != want || sh.Abilities[Cha].SaveProf != want {
			t.Errorf("level %d: Wis/Cha saves %v/%v, want %v", level, sh.Abilities[Wis].SaveProf, sh.Abilities[Cha].SaveProf, want)
		}
	}
	if r, ok := findResource(mustCompute(t, rogueAt(20, "thief"), lib, Ruleset{}), "rogue_stroke_of_luck"); !ok || r.Max != 1 || r.Recharge != RechargeShortRest {
		t.Errorf("Stroke of Luck = %+v", r)
	}
	sh := mustCompute(t, rogueAt(20, "thief"), lib, Ruleset{})
	for _, id := range []string{"rogue_asi_4", "rogue_asi_8", "rogue_asi_10", "rogue_asi_12", "rogue_asi_16", "rogue_epic_boon"} {
		if p, ok := findPending(sh, id, "feat"); !ok || p.Remaining != 1 {
			t.Errorf("%s pending = %+v (found %v)", id, p, ok)
		}
	}
	if p, ok := findPending(sh, "rogue_expertise_6", "expertise"); !ok || p.Remaining != 2 {
		t.Errorf("second Expertise pending = %+v (found %v)", p, ok)
	}
}

func TestThief(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, rogueAt(3, "thief"), lib, Ruleset{})
	noContentWarnings(t, sh)
	if a, ok := actionNamed(sh, "Fast Hands"); !ok || a.Type != ActionBonusAction {
		t.Errorf("Fast Hands = %+v", a)
	}
	if sh.Speeds[SpeedClimb] != 30 {
		t.Errorf("climb speed = %d, want 30 (equal to the walking speed)", sh.Speeds[SpeedClimb])
	}
	if _, ok := actionNamed(sh, "Cunning Strike: Stealth Attack"); ok {
		t.Error("Supreme Sneak starts at level 9")
	}
	sh = mustCompute(t, rogueAt(17, "thief"), lib, Ruleset{})
	if _, ok := actionNamed(sh, "Cunning Strike: Stealth Attack"); !ok {
		t.Error("level 17: no Stealth Attack")
	}
	for _, n := range []string{"Use Magic Device:", "Thief's Reflexes:"} {
		if _, ok := fighterNote(sh, n); !ok {
			t.Errorf("level 17: no %s note", n)
		}
	}
	if len(sh.SpellSlots) != 0 {
		t.Errorf("a thief has no spell slots: %+v", sh.SpellSlots)
	}
	// Use Magic Device (level 13) raises the attunement limit from 3 to 4.
	for level, want := range map[int]int{12: 3, 13: 4, 17: 4} {
		if got := mustCompute(t, rogueAt(level, "thief"), lib, Ruleset{}).Attunement.Max; got != want {
			t.Errorf("level %d thief: attunement limit %d, want %d", level, got, want)
		}
	}
	if got := mustCompute(t, rogueAt(13, "arcane_trickster"), lib, Ruleset{}).Attunement.Max; got != 3 {
		t.Errorf("an arcane trickster keeps the normal limit, got %d", got)
	}
}

func rogueSource(sh Sheet) (SpellSourceView, bool) {
	for _, s := range sh.Spellcasting {
		if s.ID == "class:rogue" {
			return s, true
		}
	}
	return SpellSourceView{}, false
}

func TestArcaneTricksterSlotsAndCounts(t *testing.T) {
	lib := loadElfLib(t)
	for _, tc := range []struct {
		level, cantrips, prepared int
		slots                     []int
	}{
		{3, 2, 3, []int{2}}, {4, 2, 4, []int{3}}, {7, 2, 5, []int{4, 2}}, {9, 2, 6, []int{4, 2}}, {10, 3, 7, []int{4, 3}},
		{13, 3, 9, []int{4, 3, 2}}, {16, 3, 11, []int{4, 3, 3}}, {19, 3, 12, []int{4, 3, 3, 1}}, {20, 3, 13, []int{4, 3, 3, 1}},
	} {
		sh := mustCompute(t, rogueAt(tc.level, "arcane_trickster"), lib, Ruleset{})
		noContentWarnings(t, sh)
		src, ok := rogueSource(sh)
		if !ok {
			t.Fatalf("level %d: no rogue spell source in %+v", tc.level, sh.Spellcasting)
		}
		// Int 14 (+2): DC 8 + PB + 2.
		if src.Ability != Int || src.Prep != PrepPrepared || src.SaveDC != 8+sh.ProficiencyBonus+2 {
			t.Errorf("level %d: source %+v", tc.level, src)
		}
		if src.CantripsAllowed != tc.cantrips || src.SpellsAllowed != tc.prepared {
			t.Errorf("level %d: %d cantrips and %d prepared, want %d and %d (plus Mage Hand)", tc.level, src.CantripsAllowed, src.SpellsAllowed, tc.cantrips, tc.prepared)
		}
		if !slices.ContainsFunc(src.Cantrips, func(s SpellView) bool { return s.ID == "mage_hand" }) {
			t.Errorf("level %d: Mage Hand should always be known: %+v", tc.level, src.Cantrips)
		}
		got := []int{}
		for _, s := range sh.SpellSlots {
			got = append(got, s.Max)
		}
		if !slices.Equal(got, tc.slots) {
			t.Errorf("level %d: slots %v, want %v", tc.level, got, tc.slots)
		}
	}
	// No casting before the subclass, and none for other rogues.
	for _, c := range []Character{rogueAt(2, ""), rogueAt(5, "thief")} {
		if src, ok := rogueSource(mustCompute(t, c, lib, Ruleset{})); ok {
			t.Errorf("%+v shouldn't cast: %+v", c.Classes, src)
		}
	}
}

func TestArcaneTricksterPreparesWizardSpells(t *testing.T) {
	lib := loadElfLib(t)
	c := rogueAt(7, "arcane_trickster")
	c.Spellbooks = map[string]SpellState{"class:rogue": {
		Cantrips: []string{"mind_sliver", "minor_illusion"},
		Prepared: []string{"charm_person", "disguise_self", "fog_cloud", "invisibility", "misty_step"},
	}}
	sh := mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	src, _ := rogueSource(sh)
	if len(src.Cantrips) != 3 || len(src.Spells) != 5 {
		t.Errorf("cantrips %+v, spells %+v", src.Cantrips, src.Spells)
	}
	// A level 3 spell is beyond a level 7 trickster's slots, and Cure Wounds isn't a Wizard spell.
	c.Spellbooks["class:rogue"] = SpellState{Prepared: []string{"fireball", "cure_wounds"}}
	sh = mustCompute(t, c, lib, Ruleset{})
	codes := map[WarningCode]bool{}
	for _, w := range sh.Warnings {
		codes[w.Code] = true
	}
	if !codes[WarnSpellTooHigh] || !codes[WarnNotInPool] {
		t.Errorf("warnings = %+v, want spell_too_high and not_in_pool", sh.Warnings)
	}
}

func TestArcaneTricksterFeatures(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, rogueAt(17, "arcane_trickster"), lib, Ruleset{})
	if a, ok := actionNamed(sh, "Mage Hand Legerdemain"); !ok || a.Type != ActionBonusAction {
		t.Errorf("Mage Hand Legerdemain = %+v", a)
	}
	if _, ok := fighterNote(sh, "Magical Ambush:"); !ok {
		t.Error("no Magical Ambush note")
	}
	if r, ok := findResource(sh, "arcane_trickster_spell_thief"); !ok || r.Max != 1 || r.Recharge != RechargeLongRest {
		t.Errorf("Spell Thief = %+v", r)
	}
	if a, ok := actionNamed(sh, "Spell Thief"); !ok || a.Type != ActionReaction {
		t.Errorf("Spell Thief action = %+v", a)
	}
}
