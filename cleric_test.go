package character

// cleric_test.go - tests for the Cleric in content/classes. It needs only that pack and content/example.
// Skips itself if the packs aren't there.

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

func loadClericLib(t *testing.T) *ContentLibrary {
	t.Helper()
	return loadWarlockLib(t)
}

// clericAt is a level N Life Domain cleric with Str 14, Int 10 and Wis 16 (+3).
func clericAt(level int) Character {
	return Character{
		ID: "c", Name: "Cleric",
		Abilities: map[Ability]AbilityScore{Str: {Score: 14}, Dex: {Score: 10}, Con: {Score: 14}, Int: {Score: 10}, Wis: {Score: 16}, Cha: {Score: 12}},
		Classes:   []ClassLevel{{ClassID: "cleric", SubclassID: "life", Level: level}},
	}
}

func withPick(c Character, feature, choice, pick string) Character {
	c.Selections = append(slices.Clone(c.Selections), Selection{FeatureID: feature, ChoiceID: choice, Picked: []string{pick}})
	return c
}

func clericSource(t *testing.T, sh Sheet) SpellSourceView {
	t.Helper()
	for _, s := range sh.Spellcasting {
		if s.ID == "class:cleric" {
			return s
		}
	}
	t.Fatalf("no cleric spell source in %+v", sh.Spellcasting)
	return SpellSourceView{}
}

func actionNamed(sh Sheet, name string) (Action, bool) {
	for _, a := range sh.Actions {
		if a.Name == name {
			return a, true
		}
	}
	return Action{}, false
}

func TestClericBasics(t *testing.T) {
	lib := loadClericLib(t)
	sh := mustCompute(t, clericAt(1), lib, Ruleset{})
	noContentWarnings(t, sh)
	if class, err := lib.Class("cleric"); err != nil || class.HitDie != 8 || class.Name != "Cleric" {
		t.Errorf("class = %+v (err %v)", class, err)
	}
	if !sh.Abilities[Wis].SaveProf || !sh.Abilities[Cha].SaveProf || sh.Abilities[Str].SaveProf {
		t.Errorf("saves = %+v", sh.Abilities)
	}
	for _, a := range []string{"light", "medium", "shield"} {
		if !slices.Contains(sh.ArmourProfs, a) {
			t.Errorf("armour proficiencies = %v", sh.ArmourProfs)
		}
	}
	if slices.Contains(sh.ArmourProfs, "heavy") || slices.Contains(sh.WeaponProfs, "martial") || !slices.Contains(sh.WeaponProfs, "simple") {
		t.Errorf("armour %v, weapons %v", sh.ArmourProfs, sh.WeaponProfs)
	}
	opts, err := ChoiceOptions(clericAt(1), lib, Ruleset{}, "class:cleric:start", "skills")
	if err != nil || len(opts) != 5 {
		t.Errorf("skill options = %+v (err %v)", opts, err)
	}
	if _, ok := findFeature(sh, "cleric_starting_equipment"); !ok {
		t.Error("a first-class cleric should see the starting equipment")
	}
}

// The Part B columns of the class table, level by level.
func TestClericTable(t *testing.T) {
	lib := loadClericLib(t)
	slots := [][]int{
		{2}, {3}, {4, 2}, {4, 3}, {4, 3, 2}, {4, 3, 3}, {4, 3, 3, 1}, {4, 3, 3, 2}, {4, 3, 3, 3, 1}, {4, 3, 3, 3, 2},
		{4, 3, 3, 3, 2, 1}, {4, 3, 3, 3, 2, 1}, {4, 3, 3, 3, 2, 1, 1}, {4, 3, 3, 3, 2, 1, 1}, {4, 3, 3, 3, 2, 1, 1, 1}, {4, 3, 3, 3, 2, 1, 1, 1},
		{4, 3, 3, 3, 2, 1, 1, 1, 1}, {4, 3, 3, 3, 3, 1, 1, 1, 1}, {4, 3, 3, 3, 3, 2, 1, 1, 1}, {4, 3, 3, 3, 3, 2, 2, 1, 1},
	}
	cantrips := []int{3, 3, 3, 4, 4, 4, 4, 4, 4, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5}
	prepared := []int{4, 5, 6, 7, 9, 10, 11, 12, 14, 15, 16, 16, 17, 17, 18, 18, 19, 20, 21, 22}
	channel := []int{0, 2, 2, 2, 2, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 4, 4, 4}
	for i := range slots {
		level := i + 1
		sh := mustCompute(t, clericAt(level), lib, Ruleset{})
		noContentWarnings(t, sh)
		got := []int{}
		for _, s := range sh.SpellSlots {
			got = append(got, s.Max)
		}
		if !slices.Equal(got, slots[i]) {
			t.Errorf("level %d: slots %v, want %v", level, got, slots[i])
		}
		src := clericSource(t, sh)
		if src.Ability != Wis || src.Prep != PrepPrepared || src.CantripsAllowed != cantrips[i] || src.SpellsAllowed != prepared[i] {
			t.Errorf("level %d: %+v, want %d cantrips and %d prepared", level, src, cantrips[i], prepared[i])
		}
		r, ok := findResource(sh, "cleric_channel_divinity")
		if ok != (channel[i] > 0) || r.Max != channel[i] {
			t.Errorf("level %d: Channel Divinity %+v, want %d", level, r, channel[i])
		}
		if pb := 2 + (level-1)/4; sh.ProficiencyBonus != pb {
			t.Errorf("level %d: proficiency bonus %d, want %d", level, sh.ProficiencyBonus, pb)
		}
	}
}

func TestClericFeaturesByLevel(t *testing.T) {
	lib := loadClericLib(t)
	gained := map[string]int{
		"cleric_spellcasting": 1, "cleric_divine_order": 1, "cleric_channel_divinity": 2, "life_disciple": 3, "life_domain_spells": 3, "life_preserve_life": 3,
		"cleric_asi_4": 4, "cleric_sear_undead": 5, "life_blessed_healer": 6, "cleric_blessed_strikes": 7, "cleric_asi_8": 8, "cleric_divine_intervention": 10,
		"cleric_asi_12": 12, "cleric_improved_blessed_strikes": 14, "cleric_asi_16": 16, "life_supreme_healing": 17, "cleric_epic_boon": 19, "cleric_greater_divine_intervention": 20,
	}
	for level := 1; level <= 20; level++ {
		sh := mustCompute(t, clericAt(level), lib, Ruleset{})
		for id, at := range gained {
			if _, ok := findFeature(sh, id); ok != (level >= at) {
				t.Errorf("level %d: has %s = %v, but it is gained at %d", level, id, ok, at)
			}
		}
	}
}

func TestLifeDomainSpellsAreFreeAndByLevel(t *testing.T) {
	lib := loadClericLib(t)
	has := func(level int, id string) bool {
		for _, s := range clericSource(t, mustCompute(t, clericAt(level), lib, Ruleset{})).Spells {
			if s.ID == id && s.Granted {
				return true
			}
		}
		return false
	}
	for id, at := range map[string]int{
		"aid": 3, "bless": 3, "cure_wounds_2024": 3, "lesser_restoration": 3, "mass_healing_word": 5, "revivify": 5,
		"aura_of_life": 7, "death_ward": 7, "greater_restoration": 9, "mass_cure_wounds": 9,
	} {
		if has(at-1, id) || !has(at, id) || !has(20, id) {
			t.Errorf("%s should be always prepared from level %d", id, at)
		}
	}

	// They don't count against the number prepared, and the source still asks for its own picks.
	sh := mustCompute(t, clericAt(3), lib, Ruleset{})
	if p, ok := findPending(sh, "spells:class:cleric", "prepared"); !ok || p.Remaining != 6 {
		t.Errorf("prepared pending = %+v (found %v), want 6", p, ok)
	}
}

func TestClericSpellPicksComeFromItsList(t *testing.T) {
	lib := loadClericLib(t)
	c := clericAt(1)
	c.Spellbooks = map[string]SpellState{"class:cleric": {
		Cantrips: []string{"guidance", "sacred_flame", "thaumaturgy"},
		Prepared: []string{"bless", "cure_wounds_2024", "guiding_bolt", "shield_of_faith"},
	}}
	sh := mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	src := clericSource(t, sh)
	if len(src.Cantrips) != 3 || len(src.Spells) != 4 {
		t.Fatalf("%+v", src)
	}
	if bolt, ok := findSpell(src.Spells, "guiding_bolt"); !ok || !bolt.Attack || bolt.AttackBonus != 5 || bolt.Effects[0].Dice != "4d6" {
		t.Errorf("guiding bolt = %+v, want +5 and 4d6", bolt)
	}
	if cure, ok := findSpell(src.Spells, "cure_wounds_2024"); !ok || cure.Effects[0].Dice != "2d8" || cure.Effects[0].Bonus != 3 {
		t.Errorf("cure wounds = %+v, want 2d8 + 3", cure)
	}

	c.Spellbooks["class:cleric"] = SpellState{Prepared: []string{"fireball"}}
	if sh := mustCompute(t, c, lib, Ruleset{}); !hasWarning(sh, WarnNotInPool) {
		t.Errorf("fireball isn't a cleric spell: %+v", sh.Warnings)
	}
}

func TestClericDivineOrder(t *testing.T) {
	lib := loadClericLib(t)
	opts, err := ChoiceOptions(clericAt(1), lib, Ruleset{}, "cleric_divine_order", "order")
	if err != nil || len(opts) != 2 {
		t.Errorf("orders = %+v (err %v)", opts, err)
	}

	sh := mustCompute(t, withPick(clericAt(1), "cleric_divine_order", "order", "cleric_order_protector"), lib, Ruleset{})
	if !slices.Contains(sh.ArmourProfs, "heavy") || !slices.Contains(sh.WeaponProfs, "martial") {
		t.Errorf("Protector: armour %v, weapons %v", sh.ArmourProfs, sh.WeaponProfs)
	}
	if got := clericSource(t, sh).CantripsAllowed; got != 3 {
		t.Errorf("Protector cantrips = %d", got)
	}

	base := mustCompute(t, clericAt(1), lib, Ruleset{})
	thaum := withPick(clericAt(1), "cleric_divine_order", "order", "cleric_order_thaumaturge")
	sh = mustCompute(t, thaum, lib, Ruleset{})
	if got := clericSource(t, sh).CantripsAllowed; got != 4 {
		t.Errorf("Thaumaturge cantrips = %d, want 4", got)
	}
	for _, skill := range []string{"arcana", "religion"} {
		if got := sh.Skills[skill].Bonus - base.Skills[skill].Bonus; got != 3 {
			t.Errorf("Thaumaturge adds %d to %s, want Wis 3", got, skill)
		}
	}
	if sh.Skills["history"].Bonus != base.Skills["history"].Bonus {
		t.Error("Thaumaturge only helps Arcana and Religion")
	}
	// The minimum is +1.
	thaum.Abilities = maps.Clone(thaum.Abilities)
	thaum.Abilities[Wis] = AbilityScore{Score: 8}
	if got := mustCompute(t, thaum, lib, Ruleset{}).Skills["arcana"].Bonus; got != base.Skills["arcana"].Bonus+1 {
		t.Errorf("Wis 8: arcana %d, base %d", got, base.Skills["arcana"].Bonus)
	}
}

func TestChannelDivinity(t *testing.T) {
	lib := loadClericLib(t)
	for level, dice := range map[int]string{2: "1d8", 6: "1d8", 7: "2d8", 12: "2d8", 13: "3d8", 17: "3d8", 18: "4d8", 20: "4d8"} {
		sh := mustCompute(t, clericAt(level), lib, Ruleset{})
		spark, ok := actionNamed(sh, "Divine Spark")
		if !ok || !strings.Contains(spark.Description, "Roll "+dice) || spark.ResourceID != "cleric_channel_divinity" {
			t.Errorf("level %d: Divine Spark = %+v, want %s", level, spark, dice)
		}
		if turn, ok := actionNamed(sh, "Turn Undead"); !ok || turn.ResourceID != "cleric_channel_divinity" {
			t.Errorf("level %d: Turn Undead = %+v", level, turn)
		}
		count := 0
		for _, a := range sh.Actions {
			if a.Name == "Divine Spark" {
				count++
			}
		}
		if count != 1 {
			t.Errorf("level %d: %d Divine Spark entries", level, count)
		}
	}
	if _, ok := actionNamed(mustCompute(t, clericAt(4), lib, Ruleset{}), "Sear Undead"); ok {
		t.Error("Sear Undead starts at level 5")
	}
	if _, ok := actionNamed(mustCompute(t, clericAt(5), lib, Ruleset{}), "Sear Undead"); !ok {
		t.Error("Sear Undead should be on the sheet at level 5")
	}

	// Preserve Life spends the class's uses rather than having its own.
	sh := mustCompute(t, clericAt(3), lib, Ruleset{})
	if a, ok := actionNamed(sh, "Preserve Life"); !ok || a.ResourceID != "cleric_channel_divinity" || a.ResourceFrom != "" {
		t.Errorf("Preserve Life = %+v", a)
	}
	if _, ok := findResource(sh, "life_preserve_life"); ok {
		t.Error("Preserve Life has no resource of its own")
	}
}

func TestChannelDivinityRests(t *testing.T) {
	lib := loadClericLib(t)
	c := clericAt(6)
	c.ResourceUsed = map[string]int{"cleric_channel_divinity": 3}
	rest := func(kind string) {
		t.Helper()
		if err := ApplyRest(&c, mustCompute(t, c, lib, Ruleset{}), kind); err != nil {
			t.Fatal(err)
		}
	}
	for _, left := range []int{2, 1, 0, 0} {
		rest("short")
		if got := c.ResourceUsed["cleric_channel_divinity"]; got != left {
			t.Errorf("after a short rest: %d used, want %d", got, left)
		}
	}
	c.ResourceUsed = map[string]int{"cleric_channel_divinity": 3}
	rest("long")
	if got := c.ResourceUsed["cleric_channel_divinity"]; got != 0 {
		t.Errorf("after a long rest: %d used", got)
	}
}

func TestDivineStrike(t *testing.T) {
	lib := loadClericLib(t)
	if opts, err := ChoiceOptions(clericAt(7), lib, Ruleset{}, "cleric_blessed_strikes", "strike"); err != nil || len(opts) != 2 {
		t.Errorf("Blessed Strikes options = %+v (err %v)", opts, err)
	}
	dice := func(a AttackView) []string {
		out := []string{}
		for _, r := range a.DamageRiders {
			out = append(out, r.Dice)
		}
		return out
	}
	c := withPick(clericAt(7), "cleric_blessed_strikes", "strike", "cleric_divine_strike")
	c.Inventory = []InventoryItem{equipped("mace"), equipped("light_crossbow")}
	sh := mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	for _, name := range []string{"Mace", "Light Crossbow"} {
		if got := dice(attackNamed(t, sh, name)); !slices.Equal(got, []string{"1d8"}) {
			t.Errorf("%s riders = %v", name, got)
		}
	}
	if got := dice(attackNamed(t, sh, "Unarmed Strike")); len(got) != 0 {
		t.Errorf("an unarmed strike isn't a weapon: %v", got)
	}
	if a, ok := actionNamed(sh, "Divine Strike"); !ok || !strings.Contains(a.Description, "1d8") {
		t.Errorf("Divine Strike action = %+v", a)
	}
	c.Classes[0].Level = 14
	sh = mustCompute(t, c, lib, Ruleset{})
	if got := dice(attackNamed(t, sh, "Mace")); !slices.Equal(got, []string{"2d8"}) {
		t.Errorf("level 14 riders = %v", got)
	}
	if a, ok := actionNamed(sh, "Divine Strike"); !ok || !strings.Contains(a.Description, "2d8") {
		t.Errorf("level 14 Divine Strike action = %+v", a)
	}
}

func TestPotentSpellcasting(t *testing.T) {
	lib := loadClericLib(t)
	sacred := func(c Character) SpellEffectView {
		t.Helper()
		c.Spellbooks = map[string]SpellState{"class:cleric": {Cantrips: []string{"sacred_flame", "guidance", "toll_the_dead"}, Prepared: []string{"inflict_wounds"}}}
		src := clericSource(t, mustCompute(t, c, lib, Ruleset{}))
		v, ok := findSpell(src.Cantrips, "sacred_flame")
		if !ok {
			t.Fatalf("no sacred flame in %+v", src)
		}
		wounds, _ := findSpell(src.Spells, "inflict_wounds")
		if wounds.Effects[0].Bonus != 0 {
			t.Errorf("only cantrips get the bonus: %+v", wounds.Effects[0])
		}
		return v.Effects[0]
	}
	if got := sacred(clericAt(7)); got.Bonus != 0 || got.Dice != "2d8" {
		t.Errorf("without Potent Spellcasting: %+v", got)
	}
	potent := withPick(clericAt(7), "cleric_blessed_strikes", "strike", "cleric_potent_spellcasting")
	if got := sacred(potent); got.Bonus != 3 {
		t.Errorf("with Potent Spellcasting: %+v, want Wis 3", got)
	}
	if _, ok := actionNamed(mustCompute(t, potent, lib, Ruleset{}), "Potent Spellcasting"); ok {
		t.Error("the Temporary Hit Points start at level 14")
	}
	potent.Classes[0].Level = 14
	if a, ok := actionNamed(mustCompute(t, potent, lib, Ruleset{}), "Potent Spellcasting"); !ok || !strings.Contains(a.Description, "Temporary Hit Points") {
		t.Errorf("level 14 action = %+v", a)
	}
}

func TestDivineIntervention(t *testing.T) {
	lib := loadClericLib(t)
	sh := mustCompute(t, clericAt(20), lib, Ruleset{})
	if r, ok := findResource(sh, "cleric_divine_intervention"); !ok || r.Max != 1 || r.Recharge != RechargeLongRest {
		t.Errorf("resource = %+v", r)
	}
	if a, ok := actionNamed(sh, "Greater Divine Intervention"); !ok || a.ResourceID != "cleric_divine_intervention" {
		t.Errorf("Greater Divine Intervention = %+v", a)
	}
}

func TestClericMulticlassing(t *testing.T) {
	lib := loadClericLib(t)
	c := Character{
		ID:        "m",
		Abilities: map[Ability]AbilityScore{Dex: {Score: 14}, Wis: {Score: 12}},
		Classes:   []ClassLevel{{ClassID: "rogue", Level: 3}, {ClassID: "cleric", Level: 1}},
	}
	sh := mustCompute(t, c, lib, Ruleset{})
	if !hasWarning(sh, WarnPrereqUnmet) {
		t.Errorf("Wisdom 12 should trip the prerequisite: %+v", sh.Warnings)
	}
	if _, ok := findFeature(sh, "cleric_starting_equipment"); ok {
		t.Error("starting equipment is for a first class only")
	}
	if !slices.Contains(sh.ArmourProfs, "shield") || !slices.Contains(sh.ArmourProfs, "medium") {
		t.Errorf("armour = %v", sh.ArmourProfs)
	}
	if sh.Abilities[Cha].SaveProf {
		t.Error("multiclassing doesn't give the saves")
	}
	c.Abilities[Wis] = AbilityScore{Score: 13}
	if sh := mustCompute(t, c, lib, Ruleset{}); hasWarning(sh, WarnPrereqUnmet) {
		t.Errorf("13 meets the prerequisite: %+v", sh.Warnings)
	}
}

func TestClericRuleExtensionsAreValidated(t *testing.T) {
	bad := map[string]string{
		"negative regain":      `[{"id":"f","name":"F","resource":{"name":"F","max":1,"recharge":"long_rest","short_rest_regain":-1}}]`,
		"unknown resource":     `[{"id":"f","name":"F","actions":[{"name":"A","type":"action","resource_from":"nope"}]}]`,
		"cantrip damage":       `[{"id":"f","name":"F","modifiers":[{"target":"damage.cantrip.","kind":"add","value":1}]}]`,
		"cantrips no class":    `[{"id":"f","name":"F","modifiers":[{"target":"spell.cantrips","kind":"add","value":1}]}]`,
		"weapon on non-attack": `[{"id":"f","name":"F","modifiers":[{"target":"ac","kind":"add","value":1,"requires":"weapon"}]}]`,
	}
	for name, content := range bad {
		lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{"features/f.json": content}))
		if !lib.HasErrors() {
			t.Errorf("%s should be a content error", name)
		}
	}
}
