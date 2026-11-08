package character

// bard_test.go - tests for the Bard in content/classes. It needs only that pack and content/example.
// Skips itself if the packs aren't there.

import (
	"slices"
	"strings"
	"testing"
)

// bardAt is a level N bard with Dex 14 (+2) and Cha 16 (+3), in the given college from level 3.
func bardAt(level int, college string) Character {
	cl := ClassLevel{ClassID: "bard", Level: level}
	if level >= 3 {
		cl.SubclassID = college
	}
	return Character{
		ID: "b", Name: "Bard",
		Abilities: map[Ability]AbilityScore{Str: {Score: 10}, Dex: {Score: 14}, Con: {Score: 14}, Int: {Score: 10}, Wis: {Score: 10}, Cha: {Score: 16}},
		Classes:   []ClassLevel{cl},
	}
}

func bardSource(t *testing.T, sh Sheet) SpellSourceView {
	t.Helper()
	for _, s := range sh.Spellcasting {
		if s.ID == "class:bard" {
			return s
		}
	}
	t.Fatalf("no bard spell source in %+v", sh.Spellcasting)
	return SpellSourceView{}
}

func TestBardBasics(t *testing.T) {
	lib := loadClericLib(t)
	sh := mustCompute(t, bardAt(1, ""), lib, Ruleset{})
	noContentWarnings(t, sh)
	if class, err := lib.Class("bard"); err != nil || class.HitDie != 8 || class.Name != "Bard" {
		t.Errorf("class = %+v (err %v)", class, err)
	}
	if !sh.Abilities[Dex].SaveProf || !sh.Abilities[Cha].SaveProf || sh.Abilities[Wis].SaveProf {
		t.Errorf("saves = %+v", sh.Abilities)
	}
	if !slices.Equal(sh.ArmourProfs, []string{"light"}) || !slices.Contains(sh.WeaponProfs, "simple") || slices.Contains(sh.WeaponProfs, "martial") {
		t.Errorf("armour %v, weapons %v", sh.ArmourProfs, sh.WeaponProfs)
	}
	if opts, err := ChoiceOptions(bardAt(1, ""), lib, Ruleset{}, "class:bard:start", "skills"); err != nil || len(opts) != 18 {
		t.Errorf("a bard picks from every skill: %d options (err %v)", len(opts), err)
	}
	if p, ok := findPending(sh, "class:bard:start", "instruments"); !ok || p.Remaining != 3 {
		t.Errorf("instrument pending = %+v (found %v), want 3", p, ok)
	}
	c := bardAt(1, "")
	c.Selections = []Selection{{FeatureID: "class:bard:start", ChoiceID: "instruments", Picked: []string{"lute", "flute", "drum"}}}
	sh = mustCompute(t, c, lib, Ruleset{})
	for _, id := range []string{"lute", "flute", "drum"} {
		if _, ok := sh.Tools[id]; !ok {
			t.Errorf("tools = %v, want %s", sh.Tools, id)
		}
	}
	if _, ok := findFeature(sh, "bard_starting_equipment"); !ok {
		t.Error("a first-class bard should see the starting equipment")
	}
}

// The Part B columns of the class table, level by level, plus the Bardic Inspiration column.
func TestBardTable(t *testing.T) {
	lib := loadClericLib(t)
	slots := [][]int{
		{2}, {3}, {4, 2}, {4, 3}, {4, 3, 2}, {4, 3, 3}, {4, 3, 3, 1}, {4, 3, 3, 2}, {4, 3, 3, 3, 1}, {4, 3, 3, 3, 2},
		{4, 3, 3, 3, 2, 1}, {4, 3, 3, 3, 2, 1}, {4, 3, 3, 3, 2, 1, 1}, {4, 3, 3, 3, 2, 1, 1}, {4, 3, 3, 3, 2, 1, 1, 1}, {4, 3, 3, 3, 2, 1, 1, 1},
		{4, 3, 3, 3, 2, 1, 1, 1, 1}, {4, 3, 3, 3, 3, 1, 1, 1, 1}, {4, 3, 3, 3, 3, 2, 1, 1, 1}, {4, 3, 3, 3, 3, 2, 2, 1, 1},
	}
	cantrips := []int{2, 2, 2, 3, 3, 3, 3, 3, 3, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4}
	prepared := []int{4, 5, 6, 7, 9, 10, 11, 12, 14, 15, 16, 16, 17, 17, 18, 18, 19, 20, 21, 22}
	for i := range slots {
		level := i + 1
		sh := mustCompute(t, bardAt(level, "college_of_valor"), lib, Ruleset{})
		noContentWarnings(t, sh)
		got := []int{}
		for _, s := range sh.SpellSlots {
			got = append(got, s.Max)
		}
		if !slices.Equal(got, slots[i]) {
			t.Errorf("level %d: slots %v, want %v", level, got, slots[i])
		}
		src := bardSource(t, sh)
		if src.Ability != Cha || src.Prep != PrepPrepared || src.CantripsAllowed != cantrips[i] || src.SpellsAllowed != prepared[i] {
			t.Errorf("level %d: %+v, want %d cantrips and %d prepared", level, src, cantrips[i], prepared[i])
		}
		r, ok := findResource(sh, "bard_bardic_inspiration")
		recharge := RechargeType("long_rest")
		if level >= 5 {
			recharge = "short_rest"
		}
		if !ok || r.Max != 3 || r.Recharge != recharge {
			t.Errorf("level %d: Bardic Inspiration %+v, want 3 uses on %s", level, r, recharge)
		}
		die := "d6"
		switch {
		case level >= 15:
			die = "d12"
		case level >= 10:
			die = "d10"
		case level >= 5:
			die = "d8"
		}
		if a, ok := actionNamed(sh, "Bardic Inspiration"); !ok || a.ResourceID != "bard_bardic_inspiration" || !strings.Contains(a.Description, "a "+die+" ") {
			t.Errorf("level %d: Bardic Inspiration action = %+v, want a %s", level, a, die)
		}
	}
}

func TestBardInspirationScalesWithCharisma(t *testing.T) {
	lib := loadClericLib(t)
	c := bardAt(1, "")
	c.Abilities[Cha] = AbilityScore{Score: 8}
	if r, ok := findResource(mustCompute(t, c, lib, Ruleset{}), "bard_bardic_inspiration"); !ok || r.Max != 1 {
		t.Errorf("Cha 8: %+v, want the minimum of 1", r)
	}
}

func TestBardFeaturesByLevel(t *testing.T) {
	lib := loadClericLib(t)
	common := map[string]int{
		"bard_bardic_inspiration": 1, "bard_spellcasting": 1, "bard_expertise_2": 2, "bard_jack_of_all_trades": 2, "bard_asi_4": 4,
		"bard_font_of_inspiration": 5, "bard_countercharm": 7, "bard_asi_8": 8, "bard_expertise_9": 9, "bard_magical_secrets": 10,
		"bard_asi_12": 12, "bard_asi_16": 16, "bard_superior_inspiration": 18, "bard_epic_boon": 19, "bard_words_of_creation": 20,
	}
	colleges := map[string]map[string]int{
		"college_of_valor":    {"valor_combat_inspiration": 3, "valor_martial_training": 3, "valor_extra_attack": 6, "valor_battle_magic": 14},
		"college_of_dance":    {"dance_dazzling_footwork": 3, "dance_inspired_movement": 6, "dance_tandem_footwork": 6, "dance_leading_evasion": 14},
		"college_of_the_moon": {"moon_moons_inspiration": 3, "moon_primal_lore": 3, "moon_blessing_of_moonlight": 6, "moon_eventides_splendor": 14},
	}
	for college, own := range colleges {
		for level := 1; level <= 20; level++ {
			sh := mustCompute(t, bardAt(level, college), lib, Ruleset{})
			for _, gained := range []map[string]int{common, own} {
				for id, at := range gained {
					if _, ok := findFeature(sh, id); ok != (level >= at) {
						t.Errorf("%s level %d: has %s = %v, but it is gained at %d", college, level, id, ok, at)
					}
				}
			}
		}
	}
}

func TestBardJackOfAllTradesAndExpertise(t *testing.T) {
	lib := loadClericLib(t)
	one := mustCompute(t, bardAt(1, ""), lib, Ruleset{})
	two := mustCompute(t, bardAt(2, ""), lib, Ruleset{})
	if got := two.Skills["athletics"].Bonus - one.Skills["athletics"].Bonus; got != 1 {
		t.Errorf("Jack of All Trades adds %d to an untrained skill, want 1 (half of +2)", got)
	}
	if p, ok := findPending(two, "bard_expertise_2", "expertise"); !ok || p.Remaining != 2 {
		t.Errorf("expertise pending = %+v (found %v)", p, ok)
	}
	c := bardAt(2, "")
	c.Selections = []Selection{
		{FeatureID: "class:bard:start", ChoiceID: "skills", Picked: []string{"performance", "persuasion", "insight"}},
		{FeatureID: "bard_expertise_2", ChoiceID: "expertise", Picked: []string{"performance", "persuasion"}},
	}
	sh := mustCompute(t, c, lib, Ruleset{})
	if sh.Skills["performance"].Level != ProfExpert || sh.Skills["performance"].Bonus != 3+4 {
		t.Errorf("performance = %+v, want expert at +7", sh.Skills["performance"])
	}
	if sh.Skills["insight"].Bonus != 0+2 {
		t.Errorf("insight = %+v, want trained at +2 (Jack of All Trades doesn't stack)", sh.Skills["insight"])
	}
}

func TestBardFontOfInspirationSpendsInspiration(t *testing.T) {
	lib := loadClericLib(t)
	sh := mustCompute(t, bardAt(5, "college_of_valor"), lib, Ruleset{})
	if a, ok := actionNamed(sh, "Font of Inspiration"); !ok || a.ResourceID != "bard_bardic_inspiration" {
		t.Errorf("Font of Inspiration = %+v", a)
	}
	if a, ok := actionNamed(mustCompute(t, bardAt(7, "college_of_valor"), lib, Ruleset{}), "Countercharm"); !ok || a.Type != "reaction" {
		t.Errorf("Countercharm = %+v", a)
	}
}

func TestBardWordsOfCreation(t *testing.T) {
	lib := loadClericLib(t)
	has := func(level int, college, id string) bool {
		for _, s := range bardSource(t, mustCompute(t, bardAt(level, college), lib, Ruleset{})).Spells {
			if s.ID == id && s.Granted {
				return true
			}
		}
		return false
	}
	for _, college := range []string{"college_of_valor", "college_of_dance", "college_of_the_moon"} {
		for _, id := range []string{"power_word_heal", "power_word_kill"} {
			if has(19, college, id) || !has(20, college, id) {
				t.Errorf("%s: %s should be always prepared from level 20 only", college, id)
			}
		}
	}
}

func TestBardSpellPicksComeFromItsList(t *testing.T) {
	lib := loadClericLib(t)
	c := bardAt(1, "")
	c.Spellbooks = map[string]SpellState{"class:bard": {
		Cantrips: []string{"vicious_mockery", "dancing_lights"},
		Prepared: []string{"charm_person", "color_spray", "dissonant_whispers", "healing_word"},
	}}
	sh := mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	src := bardSource(t, sh)
	if len(src.Cantrips) != 2 || len(src.Spells) != 4 {
		t.Fatalf("%+v", src)
	}
	if vm, ok := findSpell(src.Cantrips, "vicious_mockery"); !ok || vm.SaveDC != 13 || vm.Effects[0].Dice != "1d6" {
		t.Errorf("vicious mockery = %+v, want DC 13 and 1d6", vm)
	}
	c.Spellbooks["class:bard"] = SpellState{Prepared: []string{"guiding_bolt"}}
	if sh := mustCompute(t, c, lib, Ruleset{}); !hasWarning(sh, WarnNotInPool) {
		t.Errorf("guiding bolt isn't a bard spell: %+v", sh.Warnings)
	}
}

func TestCollegeOfValor(t *testing.T) {
	lib := loadClericLib(t)
	sh := mustCompute(t, bardAt(3, "college_of_valor"), lib, Ruleset{})
	if !slices.Contains(sh.WeaponProfs, "martial") || !slices.Contains(sh.ArmourProfs, "medium") || !slices.Contains(sh.ArmourProfs, "shield") {
		t.Errorf("Martial Training: armour %v, weapons %v", sh.ArmourProfs, sh.WeaponProfs)
	}
	if slices.Contains(sh.ArmourProfs, "heavy") {
		t.Error("Valor doesn't give heavy armour")
	}
	if _, ok := actionNamed(sh, "Combat Inspiration"); !ok {
		t.Error("Combat Inspiration should be on the sheet")
	}
	if _, ok := barbNote(mustCompute(t, bardAt(6, "college_of_valor"), lib, Ruleset{}), "attack.all", ModNote); !ok {
		t.Error("Extra Attack should leave a note at level 6")
	}
	if a, ok := actionNamed(mustCompute(t, bardAt(14, "college_of_valor"), lib, Ruleset{}), "Battle Magic"); !ok || a.Type != "bonus_action" {
		t.Errorf("Battle Magic = %+v", a)
	}
}

func TestCollegeOfDance(t *testing.T) {
	lib := loadClericLib(t)
	c := bardAt(3, "college_of_dance")
	sh := mustCompute(t, c, lib, Ruleset{})
	if sh.AC != 15 {
		t.Errorf("AC = %d, want 15 (10 + Dex 2 + Cha 3)", sh.AC)
	}
	if sh.Skills["performance"].Advantage {
		t.Error("dancing Advantage is situational, so it is a note, not a flat Advantage")
	}
	if a, ok := actionNamed(sh, "Agile Strikes"); !ok || a.ResourceID != "bard_bardic_inspiration" {
		t.Errorf("Agile Strikes = %+v", a)
	}
	c.Inventory = []InventoryItem{{ItemID: "shield", Quantity: 1, Equipped: true}}
	if sh := mustCompute(t, c, lib, Ruleset{}); sh.AC != 14 {
		t.Errorf("AC with a shield = %d, want 14 (10 + Dex 2, plus 2 for the shield)", sh.AC)
	}
	sh = mustCompute(t, bardAt(6, "college_of_dance"), lib, Ruleset{})
	for _, name := range []string{"Inspired Movement", "Tandem Footwork"} {
		if a, ok := actionNamed(sh, name); !ok || a.ResourceID != "bard_bardic_inspiration" {
			t.Errorf("%s = %+v", name, a)
		}
	}
	if _, ok := barbNote(mustCompute(t, bardAt(14, "college_of_dance"), lib, Ruleset{}), "save.dex", ModNote); !ok {
		t.Error("Leading Evasion should leave a note on Dexterity saves")
	}
}

func TestCollegeOfTheMoon(t *testing.T) {
	lib := loadClericLib(t)
	sh := mustCompute(t, bardAt(3, "college_of_the_moon"), lib, Ruleset{})
	noContentWarnings(t, sh)
	if !slices.Contains(sh.Languages, "druidic") {
		t.Errorf("languages = %v, want druidic", sh.Languages)
	}
	if a, ok := actionNamed(sh, "Lunar Vitality"); !ok || a.ResourceID != "bard_bardic_inspiration" {
		t.Errorf("Lunar Vitality = %+v", a)
	}
	if _, ok := actionNamed(sh, "Inspired Eclipse"); !ok {
		t.Error("Inspired Eclipse should be on the sheet")
	}
	if opts, err := ChoiceOptions(bardAt(3, "college_of_the_moon"), lib, Ruleset{}, "moon_primal_lore", "skill"); err != nil || len(opts) != 6 {
		t.Errorf("skill options = %+v (err %v)", opts, err)
	}

	// The Druid cantrip is its own spell source, so it doesn't use up a Bard cantrip.
	c := withPick(bardAt(3, "college_of_the_moon"), "moon_primal_lore", "cantrip", "thorn_whip")
	c = withPick(c, "moon_primal_lore", "skill", "nature")
	sh = mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	if sh.Skills["nature"].Level != ProfTrained {
		t.Errorf("nature = %+v", sh.Skills["nature"])
	}
	found := false
	for _, src := range sh.Spellcasting {
		if _, ok := findSpell(src.Cantrips, "thorn_whip"); ok && src.ID != "class:bard" && src.Ability == Cha {
			found = true
		}
	}
	if !found || bardSource(t, sh).CantripsAllowed != 2 {
		t.Errorf("thorn whip should be a separate Charisma source, leaving 2 Bard cantrips: %+v", sh.Spellcasting)
	}
	if !hasWarning(mustCompute(t, withPick(bardAt(3, "college_of_the_moon"), "moon_primal_lore", "cantrip", "vicious_mockery"), lib, Ruleset{}), WarnNotInPool) {
		t.Error("vicious mockery isn't a Druid cantrip")
	}

	has := func(level int) bool {
		for _, s := range bardSource(t, mustCompute(t, bardAt(level, "college_of_the_moon"), lib, Ruleset{})).Spells {
			if s.ID == "moonbeam" && s.Granted {
				return true
			}
		}
		return false
	}
	if has(5) || !has(6) {
		t.Error("Moonbeam is always prepared from level 6")
	}
	sh = mustCompute(t, bardAt(6, "college_of_the_moon"), lib, Ruleset{})
	if r, ok := findResource(sh, "moon_blessing_of_moonlight"); !ok || r.Max != 1 || r.Recharge != "long_rest" {
		t.Errorf("Blessing of Moonlight = %+v", r)
	}
	sh = mustCompute(t, bardAt(14, "college_of_the_moon"), lib, Ruleset{})
	for _, name := range []string{"Shadow of the New Moon", "Vibrance of the Full Moon"} {
		if _, ok := actionNamed(sh, name); !ok {
			t.Errorf("%s should be on the sheet at level 14", name)
		}
	}
}

func TestBardMulticlassing(t *testing.T) {
	lib := loadClericLib(t)
	c := Character{
		ID:        "m",
		Abilities: map[Ability]AbilityScore{Dex: {Score: 14}, Cha: {Score: 12}},
		Classes:   []ClassLevel{{ClassID: "rogue", Level: 3}, {ClassID: "bard", Level: 1}},
	}
	sh := mustCompute(t, c, lib, Ruleset{})
	if !hasWarning(sh, WarnPrereqUnmet) {
		t.Errorf("Charisma 12 should trip the prerequisite: %+v", sh.Warnings)
	}
	if _, ok := findFeature(sh, "bard_starting_equipment"); ok {
		t.Error("starting equipment is for a first class only")
	}
	if sh.Abilities[Cha].SaveProf {
		t.Error("multiclassing doesn't give the saves")
	}
	c.Abilities[Cha] = AbilityScore{Score: 13}
	if sh := mustCompute(t, c, lib, Ruleset{}); hasWarning(sh, WarnPrereqUnmet) {
		t.Errorf("13 meets the prerequisite: %+v", sh.Warnings)
	}
}
