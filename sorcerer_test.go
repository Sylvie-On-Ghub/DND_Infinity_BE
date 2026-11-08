package character

// sorcerer_test.go - tests for the Sorcerer, Draconic Sorcery and Wild Magic Sorcery in content/classes.
// Skips itself if the packs aren't there.

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

// sorcererAt is a level N human sorcerer with Dex 14 (+2), Con 14 (+2) and Cha 16 (+3), in the subclass from level 3.
func sorcererAt(level int, sub string, sel ...Selection) Character {
	cl := ClassLevel{ClassID: "sorcerer", Level: level}
	if level >= 3 {
		cl.SubclassID = sub
	}
	return Character{
		ID: "s", Name: "Sorcerer", RaceID: "human",
		Abilities:  map[Ability]AbilityScore{Str: {Score: 8}, Dex: {Score: 14}, Con: {Score: 14}, Int: {Score: 10}, Wis: {Score: 12}, Cha: {Score: 16}},
		Classes:    []ClassLevel{cl},
		Selections: sel,
	}
}

func sorcererSource(t *testing.T, sh Sheet) SpellSourceView {
	t.Helper()
	src, ok := findSource(sh, "class:sorcerer")
	if !ok {
		t.Fatalf("no sorcerer spell source in %+v", sh.Spellcasting)
	}
	return src
}

func TestSorcererBasics(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, sorcererAt(1, ""), lib, Ruleset{})
	noContentWarnings(t, sh)
	if class, err := lib.Class("sorcerer"); err != nil || class.HitDie != 6 || class.Name != "Sorcerer" {
		t.Errorf("class = %+v (err %v)", class, err)
	}
	if !sh.Abilities[Con].SaveProf || !sh.Abilities[Cha].SaveProf || sh.Abilities[Dex].SaveProf {
		t.Errorf("saves = %+v", sh.Abilities)
	}
	if !slices.Equal(sh.WeaponProfs, []string{"simple"}) || len(sh.ArmourProfs) != 0 {
		t.Errorf("weapons %v, armour %v", sh.WeaponProfs, sh.ArmourProfs)
	}
	if opts, err := ChoiceOptions(sorcererAt(1, ""), lib, Ruleset{}, "class:sorcerer:start", "skills"); err != nil || len(opts) != 6 {
		t.Errorf("skill options = %d (err %v), want 6", len(opts), err)
	}
	src := sorcererSource(t, sh)
	if src.Ability != Cha || src.Prep != PrepPrepared || src.SaveDC != 13 || src.AttackBonus != 5 {
		t.Errorf("source = %+v, want Cha, prepared, DC 13, +5", src)
	}
	if r, ok := findResource(sh, "sorcerer_innate_sorcery"); !ok || r.Max != 2 || r.Recharge != RechargeLongRest {
		t.Errorf("Innate Sorcery = %+v", r)
	}
	if a, ok := actionNamed(sh, "Innate Sorcery"); !ok || a.Type != ActionBonusAction {
		t.Errorf("Innate Sorcery action = %+v", a)
	}
	if _, ok := findResource(sh, "sorcerer_font_of_magic"); ok {
		t.Error("Sorcery Points start at level 2")
	}
}

// The columns of the Sorcerer Features table, level by level.
func TestSorcererTable(t *testing.T) {
	lib := loadElfLib(t)
	slots := [][]int{
		{2}, {3}, {4, 2}, {4, 3}, {4, 3, 2}, {4, 3, 3}, {4, 3, 3, 1}, {4, 3, 3, 2}, {4, 3, 3, 3, 1}, {4, 3, 3, 3, 2},
		{4, 3, 3, 3, 2, 1}, {4, 3, 3, 3, 2, 1}, {4, 3, 3, 3, 2, 1, 1}, {4, 3, 3, 3, 2, 1, 1}, {4, 3, 3, 3, 2, 1, 1, 1}, {4, 3, 3, 3, 2, 1, 1, 1},
		{4, 3, 3, 3, 2, 1, 1, 1, 1}, {4, 3, 3, 3, 3, 1, 1, 1, 1}, {4, 3, 3, 3, 3, 2, 1, 1, 1}, {4, 3, 3, 3, 3, 2, 2, 1, 1},
	}
	cantrips := []int{4, 4, 4, 5, 5, 5, 5, 5, 5, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6}
	prepared := []int{2, 4, 6, 7, 9, 10, 11, 12, 14, 15, 16, 16, 17, 17, 18, 18, 19, 20, 21, 22}
	for i := range slots {
		level := i + 1
		sh := mustCompute(t, sorcererAt(level, ""), lib, Ruleset{})
		got := []int{}
		for _, s := range sh.SpellSlots {
			got = append(got, s.Max)
		}
		if !slices.Equal(got, slots[i]) {
			t.Errorf("level %d: slots %v, want %v", level, got, slots[i])
		}
		src := sorcererSource(t, sh)
		if src.CantripsAllowed != cantrips[i] || src.SpellsAllowed != prepared[i] {
			t.Errorf("level %d: %d cantrips and %d prepared, want %d and %d", level, src.CantripsAllowed, src.SpellsAllowed, cantrips[i], prepared[i])
		}
		if level >= 2 {
			if r, ok := findResource(sh, "sorcerer_font_of_magic"); !ok || r.Max != level || r.Recharge != RechargeLongRest {
				t.Errorf("level %d: Sorcery Points = %+v, want %d", level, r, level)
			}
		}
	}
}

func TestSorcererFeaturesByLevel(t *testing.T) {
	lib := loadElfLib(t)
	for sub, gained := range map[string]map[string]int{
		"draconic": {"draconic_resilience": 3, "draconic_spells": 3, "draconic_elemental_affinity": 6,
			"draconic_dragon_wings": 14, "draconic_dragon_companion": 18},
		"wild_magic": {"wild_magic_surge": 3, "wild_magic_tides_of_chaos": 3, "wild_magic_bend_luck": 6,
			"wild_magic_controlled_chaos": 14, "wild_magic_tamed_surge": 18},
	} {
		maps.Copy(gained, map[string]int{
			"sorcerer_spellcasting": 1, "sorcerer_innate_sorcery": 1, "sorcerer_font_of_magic": 2, "sorcerer_metamagic": 2,
			"sorcerer_asi_4": 4, "sorcerer_sorcerous_restoration": 5, "sorcerer_sorcery_incarnate": 7, "sorcerer_asi_8": 8,
			"sorcerer_metamagic_10": 10, "sorcerer_asi_12": 12, "sorcerer_asi_16": 16, "sorcerer_metamagic_17": 17,
			"sorcerer_epic_boon": 19, "sorcerer_arcane_apotheosis": 20,
		})
		for level := 1; level <= 20; level++ {
			sh := mustCompute(t, sorcererAt(level, sub), lib, Ruleset{})
			for id, at := range gained {
				if _, ok := findFeature(sh, id); ok != (level >= at) {
					t.Errorf("%s level %d: has %s = %v, but it is gained at %d", sub, level, id, ok, at)
				}
			}
		}
	}
}

func TestSorcererMetamagic(t *testing.T) {
	lib := loadElfLib(t)
	opts, err := ChoiceOptions(sorcererAt(2, ""), lib, Ruleset{}, "sorcerer_metamagic", "metamagic")
	if err != nil || len(opts) != 10 {
		t.Fatalf("Metamagic options = %d (err %v), want 10", len(opts), err)
	}
	for level, want := range map[int]int{2: 2, 9: 2, 10: 4, 17: 6} {
		sh := mustCompute(t, sorcererAt(level, ""), lib, Ruleset{})
		total := 0
		for _, id := range []string{"sorcerer_metamagic", "sorcerer_metamagic_10", "sorcerer_metamagic_17"} {
			if p, ok := findPending(sh, id, "metamagic"); ok {
				total += p.Remaining
			}
		}
		if total != want {
			t.Errorf("level %d: %d Metamagic options to learn, want %d", level, total, want)
		}
	}
	c := sorcererAt(2, "", Selection{FeatureID: "sorcerer_metamagic", ChoiceID: "metamagic", Picked: []string{"metamagic_quickened", "metamagic_twinned"}})
	sh := mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	for _, name := range []string{"Quickened Spell", "Twinned Spell"} {
		if a, ok := actionNamed(sh, name); !ok || a.ResourceID != "sorcerer_font_of_magic" {
			t.Errorf("%s = %+v, want an option spending Sorcery Points", name, a)
		}
	}
	if _, ok := actionNamed(sh, "Subtle Spell"); ok {
		t.Error("Subtle Spell wasn't picked")
	}
	for level, want := range map[int]string{2: "level 1 for 2 points.", 5: "level 3 for 5.", 9: "level 5 for 7."} {
		a, ok := actionNamed(mustCompute(t, sorcererAt(level, ""), lib, Ruleset{}), "Create Spell Slot")
		if !ok || a.Type != ActionBonusAction || !strings.Contains(a.Description, want) {
			t.Errorf("level %d: Create Spell Slot = %+v, want it to end at %q", level, a, want)
		}
	}
}

func TestSorcererSpellList(t *testing.T) {
	lib := loadElfLib(t)
	c := sorcererAt(1, "")
	c.Spellbooks = map[string]SpellState{"class:sorcerer": {
		Cantrips: []string{"light", "prestidigitation", "shocking_grasp", "sorcerous_burst"},
		Prepared: []string{"burning_hands", "detect_magic"},
	}}
	sh := mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	burst, ok := findSpell(sorcererSource(t, sh).Cantrips, "sorcerous_burst")
	if !ok || !burst.Attack || burst.AttackBonus != 5 || burst.Effects[0].Dice != "1d8" {
		t.Errorf("Sorcerous Burst = %+v, want +5 and 1d8", burst)
	}
	c.Spellbooks["class:sorcerer"] = SpellState{Prepared: []string{"cure_wounds_2024"}}
	if sh := mustCompute(t, c, lib, Ruleset{}); !hasWarning(sh, WarnNotInPool) {
		t.Error("Cure Wounds isn't a sorcerer spell")
	}
}

func TestDraconicSorcery(t *testing.T) {
	lib := loadElfLib(t)
	base := mustCompute(t, sorcererAt(3, ""), lib, Ruleset{})
	sh := mustCompute(t, sorcererAt(3, "draconic"), lib, Ruleset{})
	noContentWarnings(t, sh)
	if sh.AC != 15 {
		t.Errorf("AC = %d, want 15 (10 + Dex 2 + Cha 3)", sh.AC)
	}
	if sh.MaxHP != base.MaxHP+3 {
		t.Errorf("level 3 HP = %d, want %d + 3", sh.MaxHP, base.MaxHP)
	}
	if hp20, base20 := mustCompute(t, sorcererAt(20, "draconic"), lib, Ruleset{}).MaxHP, mustCompute(t, sorcererAt(20, ""), lib, Ruleset{}).MaxHP; hp20 != base20+20 {
		t.Errorf("level 20 HP = %d, want %d + 20", hp20, base20)
	}

	has := func(level int, id string) bool {
		for _, s := range sorcererSource(t, mustCompute(t, sorcererAt(level, "draconic"), lib, Ruleset{})).Spells {
			if s.ID == id && s.Granted {
				return true
			}
		}
		return false
	}
	for id, at := range map[string]int{
		"alter_self": 3, "chromatic_orb": 3, "command": 3, "dragons_breath": 3, "fear": 5, "fly": 5,
		"arcane_eye": 7, "charm_monster": 7, "legend_lore": 9, "summon_dragon": 9,
	} {
		if has(at-1, id) || !has(at, id) || !has(20, id) {
			t.Errorf("%s should be always prepared from level %d", id, at)
		}
	}

	c := sorcererAt(6, "draconic", Selection{FeatureID: "draconic_elemental_affinity", ChoiceID: "element", Picked: []string{"fire"}})
	sh = mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	if !slices.Contains(sh.Resistances, DamageType("fire")) || slices.Contains(sh.Resistances, DamageType("cold")) {
		t.Errorf("resistances = %v, want fire", sh.Resistances)
	}
	if _, ok := barbNote(sh, "damage.spell", ModAdd); !ok {
		t.Error("no Elemental Affinity damage note")
	}

	sh = mustCompute(t, sorcererAt(18, "draconic"), lib, Ruleset{})
	if a, ok := actionNamed(sh, "Dragon Wings"); !ok || a.Type != ActionBonusAction {
		t.Errorf("Dragon Wings = %+v", a)
	}
	if _, ok := barbNote(sh, "speed.fly", ModAdd); !ok {
		t.Error("no Dragon Wings fly speed note")
	}
	if src, ok := findSource(sh, "feature:draconic_dragon_companion"); !ok || len(src.Spells) != 1 || src.Spells[0].ID != "summon_dragon" {
		t.Errorf("Dragon Companion source = %+v (found %v)", src, ok)
	}
	if r, ok := findResource(sh, "draconic_dragon_companion"); !ok || r.Max != 1 || r.Recharge != RechargeLongRest {
		t.Errorf("Dragon Companion uses = %+v", r)
	}
}

func TestWildMagicSorcery(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, sorcererAt(3, "wild_magic"), lib, Ruleset{})
	noContentWarnings(t, sh)
	if f, ok := findFeature(sh, "wild_magic_surge"); !ok || !strings.Contains(f.Description, "97-00") {
		t.Errorf("Wild Magic Surge should carry the surge table: %+v", f)
	}
	if r, ok := findResource(sh, "wild_magic_tides_of_chaos"); !ok || r.Max != 1 || r.Recharge != RechargeLongRest {
		t.Errorf("Tides of Chaos = %+v", r)
	}
	sh = mustCompute(t, sorcererAt(18, "wild_magic"), lib, Ruleset{})
	if a, ok := actionNamed(sh, "Bend Luck"); !ok || a.Type != ActionReaction || a.ResourceID != "sorcerer_font_of_magic" {
		t.Errorf("Bend Luck = %+v", a)
	}
	if r, ok := findResource(sh, "wild_magic_tamed_surge"); !ok || r.Max != 1 || r.Recharge != RechargeLongRest {
		t.Errorf("Tamed Surge = %+v", r)
	}
}

// The Sorcerer spells added after the class, shared with other lists where the 2024 rules put them.
func TestSorcererSharedSpells(t *testing.T) {
	lib := loadElfLib(t)
	all := []string{"acid_splash", "ice_knife", "arcane_vigor", "blur", "darkvision", "flaming_sphere", "gust_of_wind", "mind_spike",
		"spider_climb", "gaseous_form", "sleet_storm", "vampiric_touch", "blight", "ice_storm", "stoneskin", "vitriolic_sphere",
		"bigbys_hand", "cloudkill", "cone_of_cold", "creation", "wall_of_stone", "chain_lightning", "disintegrate", "flesh_to_stone",
		"globe_of_invulnerability", "move_earth", "otilukes_freezing_sphere", "delayed_blast_fireball", "reverse_gravity",
		"incendiary_cloud", "mass_polymorph", "meteor_swarm", "time_stop", "wish"}
	for list, want := range map[string][]string{
		"sorcerer": all, "wizard": all,
		"warlock": {"mind_spike", "spider_climb", "gaseous_form", "vampiric_touch", "blight"},
		"bard":    {"mass_polymorph"},
		"druid":   {"ice_knife", "cone_of_cold", "move_earth", "incendiary_cloud"},
	} {
		sl, err := lib.SpellList(list)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range want {
			if !slices.Contains(sl.SpellIDs, id) {
				t.Errorf("%s is missing from the %s list", id, list)
			}
		}
	}

	c := sorcererAt(11, "")
	c.Spellbooks = map[string]SpellState{"class:sorcerer": {Cantrips: []string{"acid_splash"}, Prepared: []string{"cone_of_cold", "disintegrate"}}}
	sh := mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	src := sorcererSource(t, sh)
	if s, ok := findSpell(src.Cantrips, "acid_splash"); !ok || s.Effects[0].Dice != "3d6" {
		t.Errorf("acid splash at level 11 = %+v, want 3d6", s)
	}
	if s, ok := findSpell(src.Spells, "cone_of_cold"); !ok || s.SaveDC != 15 || s.Effects[0].Dice != "8d8" {
		t.Errorf("cone of cold = %+v, want DC 15 and 8d8", s)
	}
	if s, ok := findSpell(src.Spells, "disintegrate"); !ok || s.Effects[0].Dice != "10d6" || s.Effects[0].Note != "plus 40" {
		t.Errorf("disintegrate = %+v, want 10d6 plus 40", s)
	}
}
