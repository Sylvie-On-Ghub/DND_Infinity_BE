package character

// wizard_test.go - tests for the Wizard, the Bladesinger and the Diviner in content/classes.
// Skips itself if the packs aren't there.

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

// wizardAt is a level N human wizard with Dex 14 (+2) and Int 16 (+3), in the subclass from level 3.
func wizardAt(level int, sub string, sel ...Selection) Character {
	cl := ClassLevel{ClassID: "wizard", Level: level}
	if level >= 3 {
		cl.SubclassID = sub
	}
	return Character{
		ID: "w", Name: "Wizard", RaceID: "human",
		Abilities:  map[Ability]AbilityScore{Str: {Score: 10}, Dex: {Score: 14}, Con: {Score: 14}, Int: {Score: 16}, Wis: {Score: 12}, Cha: {Score: 10}},
		Classes:    []ClassLevel{cl},
		Selections: sel,
	}
}

func wizardSource(t *testing.T, sh Sheet) SpellSourceView {
	t.Helper()
	src, ok := findSource(sh, "class:wizard")
	if !ok {
		t.Fatalf("no wizard spell source in %+v", sh.Spellcasting)
	}
	return src
}

func TestWizardBasics(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, wizardAt(1, ""), lib, Ruleset{})
	noContentWarnings(t, sh)
	if class, err := lib.Class("wizard"); err != nil || class.HitDie != 6 || class.Name != "Wizard" {
		t.Errorf("class = %+v (err %v)", class, err)
	}
	if !sh.Abilities[Int].SaveProf || !sh.Abilities[Wis].SaveProf || sh.Abilities[Dex].SaveProf {
		t.Errorf("saves = %+v", sh.Abilities)
	}
	if !slices.Equal(sh.WeaponProfs, []string{"simple"}) || len(sh.ArmourProfs) != 0 {
		t.Errorf("weapons %v, armour %v", sh.WeaponProfs, sh.ArmourProfs)
	}
	opts, err := ChoiceOptions(wizardAt(1, ""), lib, Ruleset{}, "class:wizard:start", "skills")
	if err != nil || len(opts) != 7 {
		t.Errorf("skill options = %d (err %v), want 7", len(opts), err)
	}
	for _, id := range []string{"wizard_starting_equipment", "wizard_spellcasting", "wizard_ritual_adept", "arcane_recovery"} {
		if _, ok := findFeature(sh, id); !ok {
			t.Errorf("level 1 wizard is missing %s", id)
		}
	}
	src := wizardSource(t, sh)
	if src.Ability != Int || src.Prep != PrepBook || src.SaveDC != 13 || src.AttackBonus != 5 {
		t.Errorf("source = %+v, want Int, spellbook, DC 13, +5", src)
	}
	if r, ok := findResource(sh, "arcane_recovery"); !ok || r.Max != 1 || r.Recharge != RechargeLongRest {
		t.Errorf("Arcane Recovery = %+v", r)
	}
}

// The columns of the Wizard Features table, level by level.
func TestWizardTable(t *testing.T) {
	lib := loadElfLib(t)
	slots := [][]int{
		{2}, {3}, {4, 2}, {4, 3}, {4, 3, 2}, {4, 3, 3}, {4, 3, 3, 1}, {4, 3, 3, 2}, {4, 3, 3, 3, 1}, {4, 3, 3, 3, 2},
		{4, 3, 3, 3, 2, 1}, {4, 3, 3, 3, 2, 1}, {4, 3, 3, 3, 2, 1, 1}, {4, 3, 3, 3, 2, 1, 1}, {4, 3, 3, 3, 2, 1, 1, 1}, {4, 3, 3, 3, 2, 1, 1, 1},
		{4, 3, 3, 3, 2, 1, 1, 1, 1}, {4, 3, 3, 3, 3, 1, 1, 1, 1}, {4, 3, 3, 3, 3, 2, 1, 1, 1}, {4, 3, 3, 3, 3, 2, 2, 1, 1},
	}
	cantrips := []int{3, 3, 3, 4, 4, 4, 4, 4, 4, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5}
	prepared := []int{4, 5, 6, 7, 9, 10, 11, 12, 14, 15, 16, 16, 17, 18, 19, 21, 22, 23, 24, 25}
	for i := range slots {
		level := i + 1
		sh := mustCompute(t, wizardAt(level, ""), lib, Ruleset{})
		got := []int{}
		for _, s := range sh.SpellSlots {
			got = append(got, s.Max)
		}
		if !slices.Equal(got, slots[i]) {
			t.Errorf("level %d: slots %v, want %v", level, got, slots[i])
		}
		src := wizardSource(t, sh)
		if src.CantripsAllowed != cantrips[i] || src.SpellsAllowed != prepared[i] {
			t.Errorf("level %d: %d cantrips and %d prepared, want %d and %d", level, src.CantripsAllowed, src.SpellsAllowed, cantrips[i], prepared[i])
		}
	}
}

func TestWizardFeaturesByLevel(t *testing.T) {
	lib := loadElfLib(t)
	for sub, gained := range map[string]map[string]int{
		"bladesinger": {"bladesinger_bladesong": 3, "bladesinger_training_in_war_and_song": 3, "bladesinger_extra_attack": 6,
			"bladesinger_song_of_defense": 10, "bladesinger_song_of_victory": 14},
		"diviner": {"diviner_divination_savant": 3, "diviner_portent": 3, "diviner_expert_divination": 6,
			"diviner_third_eye": 10, "diviner_greater_portent": 14},
	} {
		maps.Copy(gained, map[string]int{
			"wizard_spellcasting": 1, "wizard_ritual_adept": 1, "arcane_recovery": 1, "wizard_scholar": 2, "wizard_asi_4": 4,
			"wizard_memorize_spell": 5, "wizard_asi_8": 8, "wizard_asi_12": 12, "wizard_asi_16": 16,
			"wizard_spell_mastery": 18, "wizard_epic_boon": 19, "wizard_signature_spells": 20,
		})
		for level := 1; level <= 20; level++ {
			sh := mustCompute(t, wizardAt(level, sub), lib, Ruleset{})
			for id, at := range gained {
				if _, ok := findFeature(sh, id); ok != (level >= at) {
					t.Errorf("%s level %d: has %s = %v, but it is gained at %d", sub, level, id, ok, at)
				}
			}
		}
	}
}

func TestWizardScholar(t *testing.T) {
	lib := loadElfLib(t)
	opts, err := ChoiceOptions(wizardAt(2, ""), lib, Ruleset{}, "wizard_scholar", "expertise")
	if err != nil || len(opts) != 6 {
		t.Errorf("Scholar options = %d (err %v), want 6", len(opts), err)
	}
	c := wizardAt(2, "",
		Selection{FeatureID: "class:wizard:start", ChoiceID: "skills", Picked: []string{"arcana", "history"}},
		Selection{FeatureID: "wizard_scholar", ChoiceID: "expertise", Picked: []string{"arcana"}})
	sh := mustCompute(t, c, lib, Ruleset{})
	if sh.Skills["arcana"].Level != ProfExpert {
		t.Errorf("arcana = %+v, want Expertise", sh.Skills["arcana"])
	}
}

func TestWizardSpellbook(t *testing.T) {
	lib := loadElfLib(t)
	c := wizardAt(3, "")
	c.Spellbooks = map[string]SpellState{"class:wizard": {
		Cantrips: []string{"light", "mage_hand", "ray_of_frost"},
		Book:     []string{"detect_magic", "feather_fall", "mage_armor", "magic_missile", "sleep", "thunderwave", "misty_step", "shield"},
		Prepared: []string{"mage_armor", "magic_missile", "shield", "misty_step", "sleep", "thunderwave"},
	}}
	sh := mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	if _, ok := findPending(sh, "spells:class:wizard", "prepared"); ok {
		t.Error("six spells are prepared, which is all a level 3 wizard gets")
	}
	if class, err := lib.Class("wizard"); err != nil || class.Spellcasting == nil || !class.Spellcasting.Ritual {
		t.Errorf("wizards cast rituals from their spellbook: %+v (err %v)", class.Spellcasting, err)
	}

	c.Spellbooks["class:wizard"] = SpellState{Book: []string{"shield"}, Prepared: []string{"shield", "fireball"}}
	if sh := mustCompute(t, c, lib, Ruleset{}); !hasWarning(sh, WarnNotInPool) {
		t.Error("fireball is prepared but isn't in the spellbook")
	}
}

func TestWizardSpellMasteryAndSignatureSpells(t *testing.T) {
	lib := loadElfLib(t)
	if opts, err := ChoiceOptions(wizardAt(18, ""), lib, Ruleset{}, "wizard_spell_mastery", "level_2"); err != nil || len(opts) == 0 {
		t.Fatalf("level 2 options = %d (err %v)", len(opts), err)
	} else {
		for _, o := range opts {
			if sp, err := lib.Spell(o.OptionID); err == nil && sp.Level != 2 {
				t.Errorf("%s is level %d, not 2", o.OptionID, sp.Level)
			}
		}
	}
	c := wizardAt(20, "",
		Selection{FeatureID: "wizard_spell_mastery", ChoiceID: "level_1", Picked: []string{"magic_missile"}},
		Selection{FeatureID: "wizard_spell_mastery", ChoiceID: "level_2", Picked: []string{"scorching_ray"}},
		Selection{FeatureID: "wizard_signature_spells", ChoiceID: "spells", Picked: []string{"fireball", "counterspell"}})
	sh := mustCompute(t, c, lib, Ruleset{})
	spellIDs := func(id string) []string {
		src, ok := findSource(sh, id)
		if !ok {
			t.Fatalf("no %s source in %+v", id, sh.Spellcasting)
		}
		out := []string{}
		for _, v := range src.Spells {
			out = append(out, v.ID)
		}
		slices.Sort(out)
		return out
	}
	if got := spellIDs("feature:wizard_spell_mastery"); !slices.Equal(got, []string{"magic_missile", "scorching_ray"}) {
		t.Errorf("Spell Mastery spells = %v", got)
	}
	if got := spellIDs("feature:wizard_signature_spells"); !slices.Equal(got, []string{"counterspell", "fireball"}) {
		t.Errorf("Signature Spells = %v", got)
	}
	if r, ok := findResource(sh, "wizard_signature_spells"); !ok || r.Max != 2 || r.Recharge != RechargeShortRest {
		t.Errorf("Signature Spells uses = %+v", r)
	}
	bad := wizardAt(18, "", Selection{FeatureID: "wizard_spell_mastery", ChoiceID: "level_1", Picked: []string{"fireball"}})
	if !hasWarning(mustCompute(t, bad, lib, Ruleset{}), WarnNotInPool) {
		t.Error("fireball isn't a level 1 spell")
	}
}

func TestBladesinger(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, wizardAt(3, "bladesinger"), lib, Ruleset{})
	noContentWarnings(t, sh)
	a, ok := actionNamed(sh, "Bladesong")
	if !ok || a.Type != ActionBonusAction || a.ResourceID != "bladesinger_bladesong" {
		t.Errorf("Bladesong action = %+v", a)
	}
	if r, ok := findResource(sh, "bladesinger_bladesong"); !ok || r.Max != 3 || r.Recharge != RechargeLongRest {
		t.Errorf("Bladesong uses = %+v, want 3 (Int +3) per Long Rest", r)
	}
	for _, n := range []struct {
		target string
		kind   ModKind
	}{{"ac", ModAdd}, {"speed.walk", ModAdd}, {"skill.acrobatics", ModAdvantage}, {"save.con", ModAdd}} {
		if _, ok := barbNote(sh, n.target, n.kind); !ok {
			t.Errorf("no Bladesong note on %s", n.target)
		}
	}
	if sh.AC != 12 {
		t.Errorf("AC = %d, want 12: the Bladesong bonus only applies while it's active", sh.AC)
	}
	weapons := slices.Clone(sh.WeaponProfs)
	slices.Sort(weapons)
	if !slices.Contains(weapons, "rapier") || !slices.Contains(weapons, "longsword") || slices.Contains(weapons, "greatsword") || slices.Contains(weapons, "martial") {
		t.Errorf("weapons = %v, want one-handed, non-Heavy martial melee weapons only", weapons)
	}
	if opts, _ := ChoiceOptions(wizardAt(3, "bladesinger"), lib, Ruleset{}, "bladesinger_training_in_war_and_song", "skill"); len(opts) != 4 {
		t.Errorf("Training in War and Song skill options = %d, want 4", len(opts))
	}
	if _, ok := fighterNote(sh, "Extra Attack:"); ok {
		t.Error("Extra Attack starts at level 6")
	}

	sh = mustCompute(t, wizardAt(14, "bladesinger"), lib, Ruleset{})
	if _, ok := fighterNote(sh, "Extra Attack:"); !ok {
		t.Error("level 14: no Extra Attack note")
	}
	for name, typ := range map[string]ActionType{"Song of Defense": ActionReaction, "Song of Victory": ActionBonusAction} {
		if a, ok := actionNamed(sh, name); !ok || a.Type != typ {
			t.Errorf("%s = %+v, want a %s", name, a, typ)
		}
	}
}

func TestDiviner(t *testing.T) {
	lib := loadElfLib(t)
	for level, rolls := range map[int]int{3: 2, 13: 2, 14: 3, 20: 3} {
		sh := mustCompute(t, wizardAt(level, "diviner"), lib, Ruleset{})
		noContentWarnings(t, sh)
		if r, ok := findResource(sh, "diviner_portent"); !ok || r.Max != rolls || r.Recharge != RechargeLongRest {
			t.Errorf("level %d: Portent = %+v, want %d per Long Rest", level, r, rolls)
		}
	}
	sh := mustCompute(t, wizardAt(10, "diviner"), lib, Ruleset{})
	if a, ok := actionNamed(sh, "The Third Eye"); !ok || a.Type != ActionBonusAction || !strings.Contains(a.Description, "See Invisibility") {
		t.Errorf("Third Eye = %+v", a)
	}
	if r, ok := findResource(sh, "diviner_third_eye"); !ok || r.Max != 1 || r.Recharge != RechargeShortRest {
		t.Errorf("Third Eye uses = %+v", r)
	}
	if _, ok := fighterNote(sh, "Expert Divination:"); !ok {
		t.Error("no Expert Divination note")
	}
}
