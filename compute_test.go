package character

// compute_test.go - first-pass tests for Compute. Written without being able to run them,
// so a failure may be a mistake in the test as much as in the code: check both.
// Run with: go test ./...

import (
	"fmt"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type fakeLib struct {
	features map[string]Feature
	items    map[string]Item
	classes  map[string]Class
	spells   map[string]Spell
	lists    map[string]SpellList
}

func (l fakeLib) Feature(id string) (Feature, error) {
	if f, ok := l.features[id]; ok {
		return f, nil
	}
	return Feature{}, fmt.Errorf("no feature %q", id)
}

func (l fakeLib) Skills() []SkillDef {
	return []SkillDef{
		{ID: "athletics", Name: "Athletics", DefaultAbility: Str},
		{ID: "stealth", Name: "Stealth", DefaultAbility: Dex},
		{ID: "arcana", Name: "Arcana", DefaultAbility: Int},
		{ID: "perception", Name: "Perception", DefaultAbility: Wis},
	}
}

func (l fakeLib) Item(id string) (Item, error) {
	if it, ok := l.items[id]; ok {
		return it, nil
	}
	return Item{}, fmt.Errorf("no item %q", id)
}

func (l fakeLib) Class(id string) (Class, error) {
	if c, ok := l.classes[id]; ok {
		return c, nil
	}
	return Class{}, fmt.Errorf("no class %q", id)
}

func (l fakeLib) Spell(id string) (Spell, error) {
	if s, ok := l.spells[id]; ok {
		return s, nil
	}
	return Spell{}, fmt.Errorf("no spell %q", id)
}

func (l fakeLib) SpellList(id string) (SpellList, error) {
	if s, ok := l.lists[id]; ok {
		return s, nil
	}
	return SpellList{}, fmt.Errorf("no spell list %q", id)
}

func intp(n int) *int { return &n }

// cls builds a ClassLevel with a rolled HP entry for every level, so no HP pending item appears.
func cls(id string, level int) ClassLevel {
	cl := ClassLevel{ClassID: id, Level: level}
	for i := 0; i < level; i++ {
		cl.RolledHP = append(cl.RolledHP, 5)
	}
	return cl
}

// newChar builds a character of the given level in an unknown class "test".
// Abilities not listed default to 10 inside Compute.
func newChar(level int, scores map[Ability]int) Character {
	ab := map[Ability]AbilityScore{}
	for a, s := range scores {
		ab[a] = AbilityScore{Score: s}
	}
	return Character{ID: "c", Name: "Test", Abilities: ab, Classes: []ClassLevel{cls("test", level)}}
}

func mustCompute(t *testing.T, c Character, lib Library, rules Ruleset) Sheet {
	t.Helper()
	sh, err := Compute(c, lib, rules)
	if err != nil {
		t.Fatalf("Compute returned an error: %v", err)
	}
	return sh
}

func findWarning(sh Sheet, code WarningCode) (Warning, bool) {
	for _, w := range sh.Warnings {
		if w.Code == code {
			return w, true
		}
	}
	return Warning{}, false
}

func hasWarning(sh Sheet, code WarningCode) bool {
	_, ok := findWarning(sh, code)
	return ok
}

// hasWarningValue matches code and offending value. Most tests use an unknown class "test",
// which itself raises unknown_id, so unknown_id checks must name the thing they expect.
func hasWarningValue(sh Sheet, code WarningCode, value string) bool {
	for _, w := range sh.Warnings {
		if w.Code == code && w.Value == value {
			return true
		}
	}
	return false
}

func findAttack(sh Sheet, name string) (AttackView, bool) {
	for _, a := range sh.Attacks {
		if a.Name == name {
			return a, true
		}
	}
	return AttackView{}, false
}

func findPending(sh Sheet, featureID, choiceID string) (PendingChoice, bool) {
	for _, p := range sh.PendingChoices {
		if p.FeatureID == featureID && p.ChoiceID == choiceID {
			return p, true
		}
	}
	return PendingChoice{}, false
}

func findSource(sh Sheet, id string) (SpellSourceView, bool) {
	for _, s := range sh.Spellcasting {
		if s.ID == id {
			return s, true
		}
	}
	return SpellSourceView{}, false
}

func findSpell(list []SpellView, id string) (SpellView, bool) {
	for _, s := range list {
		if s.ID == id {
			return s, true
		}
	}
	return SpellView{}, false
}

func slotMaxes(sh Sheet) map[int]int {
	out := map[int]int{}
	for _, s := range sh.SpellSlots {
		out[s.Level] = s.Max
	}
	return out
}

// ---------------------------------------------------------------------------
// Abilities, proficiency, skills
// ---------------------------------------------------------------------------

func TestAbilityModifiersFloor(t *testing.T) {
	c := newChar(1, map[Ability]int{Str: 9, Dex: 8, Con: 7, Int: 10, Wis: 11, Cha: 12})
	sh := mustCompute(t, c, fakeLib{}, Ruleset{})
	want := map[Ability]int{Str: -1, Dex: -1, Con: -2, Int: 0, Wis: 0, Cha: 1}
	for a, w := range want {
		if got := sh.Abilities[a].Mod; got != w {
			t.Errorf("%s mod = %d, want %d", a, got, w)
		}
	}
}

func TestProficiencyBonusByLevel(t *testing.T) {
	cases := map[int]int{1: 2, 4: 2, 5: 3, 8: 3, 9: 4, 13: 5, 17: 6, 20: 6}
	for level, want := range cases {
		sh := mustCompute(t, newChar(level, nil), fakeLib{}, Ruleset{})
		if sh.ProficiencyBonus != want {
			t.Errorf("level %d: proficiency bonus = %d, want %d", level, sh.ProficiencyBonus, want)
		}
	}
}

func TestSavesSkillsAndPassives(t *testing.T) {
	c := newChar(5, map[Ability]int{Dex: 14, Int: 16}) // PB 3
	c.Abilities[Dex] = AbilityScore{Score: 14, SaveProficient: true}
	c.Skills = map[string]ProficiencyLevel{"stealth": ProfTrained, "arcana": ProfExpert}
	sh := mustCompute(t, c, fakeLib{}, Ruleset{})

	if got := sh.Abilities[Dex].SaveBonus; got != 5 { // +2 mod +3 PB
		t.Errorf("dex save = %d, want 5", got)
	}
	if got := sh.Skills["stealth"].Bonus; got != 5 {
		t.Errorf("stealth = %d, want 5", got)
	}
	if got := sh.Skills["stealth"].Passive; got != 15 {
		t.Errorf("passive stealth = %d, want 15", got)
	}
	if got := sh.Skills["arcana"].Bonus; got != 9 { // +3 mod +6 expertise
		t.Errorf("arcana = %d, want 9", got)
	}
	if got := sh.Skills["athletics"].Bonus; got != 0 {
		t.Errorf("untrained athletics = %d, want 0", got)
	}
}

func TestJackOfAllTrades(t *testing.T) {
	c := newChar(5, nil) // PB 3, so half proficiency = 1
	c.Modifiers = []Modifier{{Target: TargetJackOfAllTrades, Kind: ModSet, Value: 1, SourceID: "jack"}}
	sh := mustCompute(t, c, fakeLib{}, Ruleset{})

	if got := sh.Skills["athletics"].Bonus; got != 1 {
		t.Errorf("untrained athletics = %d, want 1", got)
	}
	if got := sh.Skills["athletics"].Level; got != ProfHalf {
		t.Errorf("untrained athletics level = %q, want %q", got, ProfHalf)
	}
	if sh.Initiative != 1 {
		t.Errorf("initiative = %d, want 1", sh.Initiative)
	}
}

func TestHPMax(t *testing.T) {
	c := newChar(3, map[Ability]int{Con: 14}) // +2 per level
	c.Classes = []ClassLevel{{ClassID: "test", Level: 3, RolledHP: []int{10, 6, 7}}}
	sh := mustCompute(t, c, fakeLib{}, Ruleset{})
	if sh.MaxHP != 29 { // 23 rolled + 3*2
		t.Errorf("max HP = %d, want 29", sh.MaxHP)
	}

	c.Modifiers = []Modifier{{Target: TargetMaxHP, Kind: ModAdd, Value: 3, SourceID: "tough"}}
	sh = mustCompute(t, c, fakeLib{}, Ruleset{})
	if sh.MaxHP != 32 {
		t.Errorf("max HP with modifier = %d, want 32", sh.MaxHP)
	}
}

// ---------------------------------------------------------------------------
// Modifiers: order of operations and stacking
// ---------------------------------------------------------------------------

func TestSetAddMinOrder(t *testing.T) {
	str := AbilityTarget(Str)
	cases := []struct {
		name string
		base int
		mods []Modifier
		want int
	}{
		{"min raises a low score", 14, []Modifier{
			{Target: str, Kind: ModMin, Value: 19, SourceID: "belt"},
			{Target: str, Kind: ModAdd, Value: 2, SourceID: "asi"},
		}, 19},
		{"min is ignored when the score is already higher", 20, []Modifier{
			{Target: str, Kind: ModMin, Value: 19, SourceID: "belt"},
			{Target: str, Kind: ModAdd, Value: 2, SourceID: "asi"},
		}, 22},
		{"set replaces the base, then add applies", 14, []Modifier{
			{Target: str, Kind: ModSet, Value: 8, SourceID: "curse"},
			{Target: str, Kind: ModAdd, Value: 2, SourceID: "asi"},
		}, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newChar(1, map[Ability]int{Str: tc.base})
			c.Modifiers = tc.mods
			sh := mustCompute(t, c, fakeLib{}, Ruleset{})
			if got := sh.Abilities[Str].Score; got != tc.want {
				t.Errorf("score = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestSameSourceDoesNotStack(t *testing.T) {
	ring := Modifier{Target: SaveTarget(Dex), Kind: ModAdd, Value: 1, SourceID: "ring_of_protection"}
	c := newChar(1, nil)
	c.Modifiers = []Modifier{ring, ring, {Target: SaveTarget(Dex), Kind: ModAdd, Value: 1, SourceID: "other_source"}}
	sh := mustCompute(t, c, fakeLib{}, Ruleset{})
	if got := sh.Abilities[Dex].SaveBonus; got != 2 { // ring once + other source once
		t.Errorf("dex save = %d, want 2", got)
	}
}

func TestAllowStackOptsOut(t *testing.T) {
	m := Modifier{Target: SaveTarget(Dex), Kind: ModAdd, Value: 1, SourceID: "homebrew_charm", AllowStack: true}
	c := newChar(1, nil)
	c.Modifiers = []Modifier{m, m}
	sh := mustCompute(t, c, fakeLib{}, Ruleset{})
	if got := sh.Abilities[Dex].SaveBonus; got != 2 {
		t.Errorf("dex save = %d, want 2", got)
	}
}

func TestUnknownTargetWarns(t *testing.T) {
	c := newChar(1, nil)
	c.Modifiers = []Modifier{{Target: "skil.stealth", Kind: ModAdd, Value: 5, SourceID: "typo"}}
	sh := mustCompute(t, c, fakeLib{}, Ruleset{})
	if !hasWarning(sh, WarnUnknownTarget) {
		t.Errorf("expected an unknown_target warning, got %+v", sh.Warnings)
	}
}

// ---------------------------------------------------------------------------
// Armour class
// ---------------------------------------------------------------------------

func acLib() fakeLib {
	return fakeLib{
		features: map[string]Feature{
			"barb_ud": {ID: "barb_ud", Name: "Unarmored Defense", ACFormulas: []ACFormula{{
				ID: "ud_barb", Name: "Unarmored Defense (Barbarian)", NoArmour: true,
				Formula: Formula{Base: 10, Terms: []Term{{Source: "mod.dex"}, {Source: "mod.con"}}},
			}}},
		},
		items: map[string]Item{
			"chain_mail": {ID: "chain_mail", Name: "Chain Mail", Kind: ItemArmour,
				ACFormula: &ACFormula{ID: "chain_mail", Name: "Chain Mail", Formula: Formula{Base: 16}}},
			"half_plate": {ID: "half_plate", Name: "Half Plate", Kind: ItemArmour,
				ACFormula: &ACFormula{ID: "half_plate", Name: "Half Plate",
					Formula: Formula{Base: 15, Terms: []Term{{Source: "mod.dex", Max: intp(2)}}}}},
			"shield": {ID: "shield", Name: "Shield", Kind: ItemShield,
				Modifiers: []Modifier{{Target: TargetAC, Kind: ModAdd, Value: 2}}},
		},
	}
}

func equip(c *Character, ids ...string) {
	for _, id := range ids {
		c.Inventory = append(c.Inventory, InventoryItem{ItemID: id, Quantity: 1, Equipped: true})
	}
}

func TestACFormulas(t *testing.T) {
	cases := []struct {
		name      string
		features  []string
		equipment []string
		dex       int
		override  string
		wantAC    int
		wantID    string
	}{
		{"baseline 10 + Dex", nil, nil, 14, "", 12, "base"},
		{"barbarian unarmoured defense wins", []string{"barb_ud"}, nil, 14, "", 15, "ud_barb"},
		{"armour makes unarmoured formulas ineligible", []string{"barb_ud"}, []string{"chain_mail"}, 14, "", 16, "chain_mail"},
		{"shield adds on top", []string{"barb_ud"}, []string{"chain_mail", "shield"}, 14, "", 18, "chain_mail"},
		{"barbarian keeps a shield", []string{"barb_ud"}, []string{"shield"}, 14, "", 17, "ud_barb"},
		{"medium armour caps Dex", nil, []string{"half_plate"}, 18, "", 17, "half_plate"},
		{"override picks an ineligible formula", []string{"barb_ud"}, []string{"chain_mail"}, 14, "ud_barb", 15, "ud_barb"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newChar(5, map[Ability]int{Dex: tc.dex, Con: 16})
			c.FeatureIDs = tc.features
			equip(&c, tc.equipment...)
			c.ACFormulaOverride = tc.override
			sh := mustCompute(t, c, acLib(), Ruleset{})

			if sh.AC != tc.wantAC {
				t.Errorf("AC = %d, want %d", sh.AC, tc.wantAC)
			}
			if sh.ACDetail.FormulaID != tc.wantID {
				t.Errorf("formula = %q, want %q", sh.ACDetail.FormulaID, tc.wantID)
			}
			if tc.override != "" {
				if !sh.ACDetail.Overridden {
					t.Error("expected Overridden to be true")
				}
				if !hasWarning(sh, WarnACRequirementUnmet) {
					t.Error("expected an ac_requirement_unmet warning for the forced formula")
				}
			}
		})
	}
}

func TestACOptionsExplainIneligible(t *testing.T) {
	c := newChar(5, map[Ability]int{Dex: 14, Con: 16})
	c.FeatureIDs = []string{"barb_ud"}
	equip(&c, "chain_mail")
	sh := mustCompute(t, c, acLib(), Ruleset{})

	found := false
	for _, o := range sh.ACDetail.Options {
		if o.ID == "ud_barb" {
			found = true
			if o.Eligible || o.Reason == "" {
				t.Errorf("ud_barb should be ineligible with a reason, got %+v", o)
			}
		}
	}
	if !found {
		t.Error("ud_barb missing from AC options")
	}
}

// ---------------------------------------------------------------------------
// Weapons and dice riders
// ---------------------------------------------------------------------------

func weaponLib() fakeLib {
	return fakeLib{items: map[string]Item{
		"longsword": {ID: "longsword", Name: "Longsword", Kind: ItemWeapon, Weapon: &WeaponStats{
			Category: "martial", VersatileDice: "1d10", MagicBonus: 1,
			Attack: AttackDetails{DamageDice: "1d8", DamageType: "slashing", RangeNormal: 5, Properties: []string{"versatile"}},
		}},
		"attuned_blade": {ID: "attuned_blade", Name: "Attuned Blade", Kind: ItemWeapon, RequiresAttunement: true, Weapon: &WeaponStats{
			Category: "martial", MagicBonus: 1,
			Attack: AttackDetails{DamageDice: "1d8", DamageType: "slashing", RangeNormal: 5},
		}},
		"rapier": {ID: "rapier", Name: "Rapier", Kind: ItemWeapon, Weapon: &WeaponStats{
			Category: "martial",
			Attack:   AttackDetails{DamageDice: "1d8", DamageType: "piercing", RangeNormal: 5, Properties: []string{"finesse"}},
		}},
	}}
}

func TestWeaponAttackMath(t *testing.T) {
	c := newChar(5, map[Ability]int{Str: 16}) // +3 mod, PB 3
	c.WeaponProfs = []string{"martial"}
	equip(&c, "longsword")
	sh := mustCompute(t, c, weaponLib(), Ruleset{})

	a, ok := findAttack(sh, "Longsword")
	if !ok {
		t.Fatalf("no Longsword attack in %+v", sh.Attacks)
	}
	if a.ToHit != 7 { // 3 mod + 3 PB + 1 magic
		t.Errorf("to hit = %d, want 7", a.ToHit)
	}
	if a.DamageBonus != 4 { // 3 mod + 1 magic
		t.Errorf("damage bonus = %d, want 4", a.DamageBonus)
	}
	if !a.Proficient || a.Ability != Str || a.VersatileDice != "1d10" {
		t.Errorf("unexpected attack: %+v", a)
	}
}

func TestWeaponWithoutProficiency(t *testing.T) {
	c := newChar(5, map[Ability]int{Str: 16})
	equip(&c, "longsword")
	sh := mustCompute(t, c, weaponLib(), Ruleset{})
	a, _ := findAttack(sh, "Longsword")
	if a.Proficient || a.ToHit != 4 { // 3 mod + 1 magic, no PB
		t.Errorf("non-proficient attack = %+v, want to hit 4 and not proficient", a)
	}
}

func TestFinesseUsesBetterAbility(t *testing.T) {
	c := newChar(1, map[Ability]int{Str: 10, Dex: 16})
	equip(&c, "rapier")
	sh := mustCompute(t, c, weaponLib(), Ruleset{})
	a, _ := findAttack(sh, "Rapier")
	if a.Ability != Dex {
		t.Errorf("rapier ability = %q, want dex", a.Ability)
	}
}

func TestUnattunedWeaponLosesMagicBonus(t *testing.T) {
	c := newChar(5, map[Ability]int{Str: 16})
	c.WeaponProfs = []string{"martial"}
	equip(&c, "attuned_blade")
	sh := mustCompute(t, c, weaponLib(), Ruleset{})

	a, _ := findAttack(sh, "Attuned Blade")
	if a.ToHit != 6 || a.DamageBonus != 3 { // no +1
		t.Errorf("unattuned blade = to hit %d, damage %d; want 6 and 3", a.ToHit, a.DamageBonus)
	}
	if !hasWarning(sh, WarnNotAttuned) {
		t.Error("expected a not_attuned warning")
	}

	c.Inventory[0].Attuned = true
	sh = mustCompute(t, c, weaponLib(), Ruleset{})
	a, _ = findAttack(sh, "Attuned Blade")
	if a.ToHit != 7 || a.DamageBonus != 4 {
		t.Errorf("attuned blade = to hit %d, damage %d; want 7 and 4", a.ToHit, a.DamageBonus)
	}
}

func TestUnarmedStrikeAlwaysPresent(t *testing.T) {
	c := newChar(5, map[Ability]int{Str: 16})
	sh := mustCompute(t, c, fakeLib{}, Ruleset{})
	a, ok := findAttack(sh, "Unarmed Strike")
	if !ok {
		t.Fatal("no Unarmed Strike")
	}
	if a.ToHit != 6 || a.DamageBonus != 4 { // 3 mod + 3 PB; 1 flat + 3 mod
		t.Errorf("unarmed = to hit %d, damage %d; want 6 and 4", a.ToHit, a.DamageBonus)
	}
}

func TestDiceRiderAppearsOnAttackNotInNumbers(t *testing.T) {
	c := newChar(5, map[Ability]int{Str: 16})
	c.WeaponProfs = []string{"martial"}
	equip(&c, "longsword")
	c.Modifiers = []Modifier{{Target: TargetDamageMelee, Kind: ModAdd, Dice: "1d6", SourceID: "hunters_mark"}}
	sh := mustCompute(t, c, weaponLib(), Ruleset{})

	a, _ := findAttack(sh, "Longsword")
	if a.DamageBonus != 4 {
		t.Errorf("damage bonus = %d, want 4 (riders are not summed)", a.DamageBonus)
	}
	if len(a.DamageRiders) != 1 || a.DamageRiders[0].Dice != "1d6" {
		t.Errorf("damage riders = %+v, want one 1d6", a.DamageRiders)
	}
	if len(sh.Notes) != 0 {
		t.Errorf("attack riders should not also appear in Notes, got %+v", sh.Notes)
	}
}

func TestDiceOnOtherTargetsGoToNotes(t *testing.T) {
	c := newChar(1, nil)
	c.Modifiers = []Modifier{{Target: SaveTarget(Dex), Kind: ModAdd, Dice: "1d4", SourceID: "bless_like"}}
	sh := mustCompute(t, c, fakeLib{}, Ruleset{})
	if got := sh.Abilities[Dex].SaveBonus; got != 0 {
		t.Errorf("dex save = %d, want 0 (dice are not summed)", got)
	}
	if len(sh.Notes) != 1 || sh.Notes[0].Dice != "1d4" {
		t.Errorf("notes = %+v, want the 1d4 rider", sh.Notes)
	}
}

func TestBadDiceWarns(t *testing.T) {
	c := newChar(1, nil)
	c.Modifiers = []Modifier{{Target: TargetDamageMelee, Kind: ModAdd, Dice: "d6", SourceID: "typo"}}
	sh := mustCompute(t, c, fakeLib{}, Ruleset{})
	if !hasWarning(sh, WarnBadDice) {
		t.Errorf("expected a bad_dice warning, got %+v", sh.Warnings)
	}
}

// ---------------------------------------------------------------------------
// Choices: permissive validation, pending items, groups
// ---------------------------------------------------------------------------

func stylesLib() fakeLib {
	return fakeLib{features: map[string]Feature{
		"fighting_style": {ID: "fighting_style", Name: "Fighting Style", Choices: []Choice{{
			ID: "style", Prompt: "Choose a fighting style", Kind: ChoicePick, Count: 1,
			Options: []Option{
				{ID: "dueling", Name: "Dueling", Modifiers: []Modifier{{Target: TargetDamageMelee, Kind: ModAdd, Value: 2}}},
				{ID: "archery", Name: "Archery", Modifiers: []Modifier{{Target: TargetAttackRanged, Kind: ModAdd, Value: 2}}},
			},
		}}},
	}}
}

func styleChar(picked ...string) Character {
	c := newChar(1, nil)
	c.FeatureIDs = []string{"fighting_style"}
	if picked != nil {
		c.Selections = []Selection{{FeatureID: "fighting_style", ChoiceID: "style", Picked: picked}}
	}
	return c
}

func TestChoicePendingUntilMade(t *testing.T) {
	sh := mustCompute(t, styleChar(), stylesLib(), Ruleset{})
	p, ok := findPending(sh, "fighting_style", "style")
	if !ok || p.Remaining != 1 {
		t.Errorf("expected one pending fighting style, got %+v", sh.PendingChoices)
	}
}

func TestChoiceAppliesPickedOption(t *testing.T) {
	sh := mustCompute(t, styleChar("dueling"), stylesLib(), Ruleset{})
	if _, ok := findPending(sh, "fighting_style", "style"); ok {
		t.Error("choice should no longer be pending")
	}
	a, _ := findAttack(sh, "Unarmed Strike")
	if a.DamageBonus != 3 { // 1 flat + 0 mod + 2 dueling
		t.Errorf("unarmed damage bonus = %d, want 3", a.DamageBonus)
	}
}

func TestChoiceOutsidePoolWarnsButDoesNotFail(t *testing.T) {
	sh := mustCompute(t, styleChar("bogus"), stylesLib(), Ruleset{})
	if !hasWarning(sh, WarnNotInPool) {
		t.Errorf("expected not_in_pool, got %+v", sh.Warnings)
	}
}

func TestTooManyPicksWarnsButApplies(t *testing.T) {
	sh := mustCompute(t, styleChar("dueling", "archery"), stylesLib(), Ruleset{})
	if !hasWarning(sh, WarnOverCount) {
		t.Errorf("expected over_count, got %+v", sh.Warnings)
	}
	a, _ := findAttack(sh, "Unarmed Strike")
	if a.DamageBonus != 3 {
		t.Errorf("dueling should still apply, damage bonus = %d, want 3", a.DamageBonus)
	}
}

func asiLib() fakeLib {
	return fakeLib{features: map[string]Feature{
		"asi4": {ID: "asi4", Name: "Ability Score Improvement", Choices: []Choice{
			{
				ID: "scores", Prompt: "Increase abilities", Kind: ChoiceAllocate, Points: 2, MaxPerOption: 2, Group: "asi_or_feat",
				Options: []Option{
					{ID: "str", Name: "Strength", Modifiers: []Modifier{{Target: AbilityTarget(Str), Kind: ModAdd, Value: 1}}},
					{ID: "dex", Name: "Dexterity", Modifiers: []Modifier{{Target: AbilityTarget(Dex), Kind: ModAdd, Value: 1}}},
				},
			},
			{ID: "feat", Prompt: "Take a feat", Kind: ChoicePick, Count: 1, Group: "asi_or_feat", Filter: &PoolFilter{Type: "feat"}},
		}},
		"tough": {ID: "tough", Name: "Tough"},
	}}
}

func asiChar(sels ...Selection) Character {
	c := newChar(4, map[Ability]int{Str: 14})
	c.FeatureIDs = []string{"asi4"}
	c.Selections = sels
	return c
}

func TestChoiceGroupBothPendingWhenEmpty(t *testing.T) {
	sh := mustCompute(t, asiChar(), asiLib(), Ruleset{})
	if _, ok := findPending(sh, "asi4", "scores"); !ok {
		t.Error("expected the ability allocation to be pending")
	}
	if _, ok := findPending(sh, "asi4", "feat"); !ok {
		t.Error("expected the feat pick to be pending")
	}
}

func TestChoiceGroupAllocationSatisfiesFeat(t *testing.T) {
	sh := mustCompute(t, asiChar(Selection{FeatureID: "asi4", ChoiceID: "scores", Allocated: map[string]int{"str": 2}}), asiLib(), Ruleset{})
	if got := sh.Abilities[Str].Score; got != 16 {
		t.Errorf("Str = %d, want 16", got)
	}
	if _, ok := findPending(sh, "asi4", "feat"); ok {
		t.Error("feat should no longer be pending once the allocation is done")
	}
	if _, ok := findPending(sh, "asi4", "scores"); ok {
		t.Error("allocation should not be pending")
	}
}

func TestChoiceGroupFeatSatisfiesAllocation(t *testing.T) {
	sh := mustCompute(t, asiChar(Selection{FeatureID: "asi4", ChoiceID: "feat", Picked: []string{"tough"}}), asiLib(), Ruleset{})
	if _, ok := findPending(sh, "asi4", "scores"); ok {
		t.Error("allocation should not be pending once a feat is picked")
	}
	if hasWarningValue(sh, WarnUnknownID, "tough") {
		t.Errorf("tough should resolve, got %+v", sh.Warnings)
	}
}

func TestChoiceGroupBothFilledWarns(t *testing.T) {
	sh := mustCompute(t, asiChar(
		Selection{FeatureID: "asi4", ChoiceID: "scores", Allocated: map[string]int{"str": 2}},
		Selection{FeatureID: "asi4", ChoiceID: "feat", Picked: []string{"tough"}},
	), asiLib(), Ruleset{})
	w, ok := findWarning(sh, WarnOverCount)
	if !ok || w.Value != "group:asi_or_feat" {
		t.Errorf("expected a group over_count warning, got %+v", sh.Warnings)
	}
}

func TestUnknownFeatureWarns(t *testing.T) {
	c := newChar(1, nil)
	c.FeatureIDs = []string{"missing"}
	sh := mustCompute(t, c, fakeLib{}, Ruleset{})
	if !hasWarningValue(sh, WarnUnknownID, "missing") {
		t.Errorf("expected unknown_id, got %+v", sh.Warnings)
	}
}

func TestFeaturePrerequisites(t *testing.T) {
	lib := fakeLib{features: map[string]Feature{
		"heavy_thing": {ID: "heavy_thing", Name: "Heavy Thing", Prerequisites: []Prerequisite{
			{Description: "Strength 13", Target: "ability.str", Min: 13},
		}},
		"vague_thing": {ID: "vague_thing", Name: "Vague Thing", Prerequisites: []Prerequisite{
			{Description: "the DM's blessing"},
		}},
	}}
	c := newChar(1, map[Ability]int{Str: 10})
	c.FeatureIDs = []string{"heavy_thing", "vague_thing"}
	sh := mustCompute(t, c, lib, Ruleset{})
	if !hasWarning(sh, WarnPrereqUnmet) {
		t.Error("expected prereq_unmet for Strength 13")
	}
	if !hasWarning(sh, WarnPrereqUnverifiable) {
		t.Error("expected prereq_unverifiable for the free-text prerequisite")
	}

	c.Abilities[Str] = AbilityScore{Score: 14}
	sh = mustCompute(t, c, lib, Ruleset{})
	if hasWarning(sh, WarnPrereqUnmet) {
		t.Error("prereq_unmet should clear once Strength is 14")
	}
}

// ---------------------------------------------------------------------------
// Classes: tiers, tables, saves, pending items, multiclassing, resources
// ---------------------------------------------------------------------------

func sneakTier(dice string, min int) Tier {
	return Tier{MinLevel: min, Modifiers: []Modifier{{Target: TargetDamageMelee, Kind: ModAdd, Dice: dice, Condition: "once per turn"}}}
}

func rogueLib() fakeLib {
	return fakeLib{
		features: map[string]Feature{
			"sneak_attack": {ID: "sneak_attack", Name: "Sneak Attack", Tiers: []Tier{
				sneakTier("1d6", 1), sneakTier("2d6", 3), sneakTier("3d6", 5),
			}},
		},
		classes: map[string]Class{
			"rogue": {
				ID: "rogue", Name: "Rogue", HitDie: 8,
				StartingGrants: Grants{Saves: []Ability{Dex, Int}},
				StartingChoices: []Choice{{
					ID: "skills", Prompt: "Choose 2 skills", Kind: ChoicePick, Count: 2,
					Filter: &PoolFilter{Type: "skill", Allowed: []string{"stealth", "perception"}},
				}},
				Levels:        [][]string{{"sneak_attack"}, {}, {}, {}, {}},
				SubclassLevel: 3,
				Subclasses:    []Subclass{{ID: "thief", Name: "Thief"}},
			},
			"wizard": {
				ID: "wizard", Name: "Wizard", HitDie: 6,
				StartingGrants:   Grants{Saves: []Ability{Int, Wis}},
				MulticlassPrereq: []Prerequisite{{Description: "Intelligence 13", Target: "ability.int", Min: 13}},
				Levels:           [][]string{{}},
			},
		},
	}
}

func rogueChar(level int) Character {
	c := newChar(level, nil)
	c.Classes = []ClassLevel{cls("rogue", level)}
	return c
}

func TestTierSelectionByClassLevel(t *testing.T) {
	cases := map[int]string{1: "1d6", 2: "1d6", 3: "2d6", 4: "2d6", 5: "3d6"}
	for level, want := range cases {
		t.Run(fmt.Sprintf("level %d", level), func(t *testing.T) {
			sh := mustCompute(t, rogueChar(level), rogueLib(), Ruleset{})
			a, _ := findAttack(sh, "Unarmed Strike")
			if len(a.DamageRiders) != 1 || a.DamageRiders[0].Dice != want {
				t.Errorf("riders = %+v, want exactly one %s (tiers replace, never stack)", a.DamageRiders, want)
			}
			if len(a.DamageRiders) == 1 && a.DamageRiders[0].Condition != "once per turn" {
				t.Errorf("rider condition = %q", a.DamageRiders[0].Condition)
			}
		})
	}
}

func TestFirstClassGrantsSavesAndStartingChoices(t *testing.T) {
	sh := mustCompute(t, rogueChar(1), rogueLib(), Ruleset{})
	if !sh.Abilities[Dex].SaveProf || !sh.Abilities[Int].SaveProf {
		t.Error("rogue should grant Dex and Int save proficiency")
	}
	if sh.Abilities[Wis].SaveProf {
		t.Error("Wis save should not be proficient")
	}
	p, ok := findPending(sh, "class:rogue:start", "skills")
	if !ok || p.Remaining != 2 {
		t.Errorf("expected 2 pending starting skills, got %+v", sh.PendingChoices)
	}
}

func TestStartingSkillPicks(t *testing.T) {
	c := rogueChar(1)
	c.Selections = []Selection{{FeatureID: "class:rogue:start", ChoiceID: "skills", Picked: []string{"stealth"}}}
	sh := mustCompute(t, c, rogueLib(), Ruleset{})
	if sh.Skills["stealth"].Level != ProfTrained {
		t.Errorf("stealth level = %q, want trained", sh.Skills["stealth"].Level)
	}
	if p, ok := findPending(sh, "class:rogue:start", "skills"); !ok || p.Remaining != 1 {
		t.Errorf("expected 1 skill still pending, got %+v", sh.PendingChoices)
	}

	// Permissive: an out-of-list pick warns but still applies.
	c.Selections[0].Picked = []string{"stealth", "athletics"}
	sh = mustCompute(t, c, rogueLib(), Ruleset{})
	if !hasWarning(sh, WarnNotInPool) {
		t.Error("expected not_in_pool for athletics")
	}
	if sh.Skills["athletics"].Level != ProfTrained {
		t.Error("athletics should still be applied")
	}
}

func TestSubclassPendingOnceUnlocked(t *testing.T) {
	if _, ok := findPending(mustCompute(t, rogueChar(2), rogueLib(), Ruleset{}), "class:rogue", "subclass"); ok {
		t.Error("subclass should not be pending at level 2")
	}
	if _, ok := findPending(mustCompute(t, rogueChar(3), rogueLib(), Ruleset{}), "class:rogue", "subclass"); !ok {
		t.Error("subclass should be pending at level 3")
	}
	c := rogueChar(3)
	c.Classes[0].SubclassID = "thief"
	if _, ok := findPending(mustCompute(t, c, rogueLib(), Ruleset{}), "class:rogue", "subclass"); ok {
		t.Error("subclass should not be pending once chosen")
	}
}

func TestMissingRolledHPIsPending(t *testing.T) {
	c := rogueChar(5)
	c.Classes[0].RolledHP = []int{8, 5, 5}
	sh := mustCompute(t, c, rogueLib(), Ruleset{})
	p, ok := findPending(sh, "class:rogue", "rolled_hp")
	if !ok || p.Remaining != 2 {
		t.Errorf("expected 2 HP entries pending, got %+v", sh.PendingChoices)
	}
}

func TestMulticlassOnlyFirstClassGrantsSaves(t *testing.T) {
	c := newChar(4, map[Ability]int{Int: 10})
	c.Classes = []ClassLevel{cls("rogue", 3), cls("wizard", 1)}
	sh := mustCompute(t, c, rogueLib(), Ruleset{})
	if sh.Abilities[Wis].SaveProf {
		t.Error("the second class must not grant its save proficiencies")
	}
	if !hasWarning(sh, WarnPrereqUnmet) {
		t.Error("expected the wizard multiclass prerequisite (Int 13) to warn")
	}

	c.Abilities[Int] = AbilityScore{Score: 13}
	sh = mustCompute(t, c, rogueLib(), Ruleset{})
	if hasWarning(sh, WarnPrereqUnmet) {
		t.Error("prerequisite should be met at Int 13")
	}
}

func TestTierResourceAndUsage(t *testing.T) {
	rageRes := func(max int) *Resource { return &Resource{Name: "Rage", Max: max, Recharge: RechargeLongRest} }
	lib := fakeLib{
		features: map[string]Feature{
			"rage": {ID: "rage", Name: "Rage", Resource: rageRes(2), Tiers: []Tier{
				{MinLevel: 3, Resource: rageRes(3)},
				{MinLevel: 6, Resource: rageRes(4)},
			}},
		},
		classes: map[string]Class{
			"barbarian": {ID: "barbarian", Name: "Barbarian", Levels: [][]string{{"rage"}, {}, {}, {}, {}, {}}},
		},
	}
	find := func(sh Sheet) (ResourceView, bool) {
		for _, r := range sh.Resources {
			if r.FeatureID == "rage" {
				return r, true
			}
		}
		return ResourceView{}, false
	}

	c := newChar(6, nil)
	c.Classes = []ClassLevel{cls("barbarian", 6)}
	c.ResourceUsed = map[string]int{"rage": 1}
	r, ok := find(mustCompute(t, c, lib, Ruleset{}))
	if !ok || r.Max != 4 || r.Used != 1 || r.Remaining != 3 {
		t.Errorf("level 6 rage = %+v, want max 4, used 1, remaining 3", r)
	}

	c.Classes = []ClassLevel{cls("barbarian", 2)}
	r, _ = find(mustCompute(t, c, lib, Ruleset{}))
	if r.Max != 2 {
		t.Errorf("level 2 rage max = %d, want 2", r.Max)
	}

	c.Classes = []ClassLevel{cls("barbarian", 6)}
	c.ResourceUsed = map[string]int{"rage": 5}
	sh := mustCompute(t, c, lib, Ruleset{})
	r, _ = find(sh)
	if r.Remaining != 0 || !hasWarning(sh, WarnOverCap) {
		t.Errorf("overspent rage = %+v, want remaining 0 and an over_cap warning", r)
	}
}

// ---------------------------------------------------------------------------
// Spells
// ---------------------------------------------------------------------------

func casterLib() fakeLib {
	wizardList := SpellList{ID: "wizard", Name: "Wizard", SpellIDs: []string{"fire_bolt", "fireball", "magic_missile"}}
	return fakeLib{
		features: map[string]Feature{
			"fire_breath": {ID: "fire_breath", Name: "Fire Breath", Spells: &SpellGrant{SpellIDs: []string{"fire_bolt"}, Ability: Cha}},
		},
		classes: map[string]Class{
			"wizard": {ID: "wizard", Name: "Wizard", Spellcasting: &SpellcastingDef{
				Ability: Int, Divisor: 1, Prep: PrepBook, SpellListID: "wizard",
				CantripsKnown:   []int{4},
				PreparedFormula: &Formula{Terms: []Term{{Source: "mod.int"}, {Source: "class_level.wizard"}}},
			}},
			"cleric": {ID: "cleric", Name: "Cleric", Spellcasting: &SpellcastingDef{Ability: Wis, Divisor: 1, Prep: PrepPrepared}},
			"paladin": {ID: "paladin", Name: "Paladin", Spellcasting: &SpellcastingDef{
				Ability: Cha, Divisor: 2, StartLevel: 2, Prep: PrepPrepared,
			}},
			"warlock": {ID: "warlock", Name: "Warlock", Spellcasting: &SpellcastingDef{Ability: Cha, Pact: true, Prep: PrepKnown}},
		},
		spells: map[string]Spell{
			"fire_bolt": {ID: "fire_bolt", Name: "Fire Bolt", Level: 0, Attack: true, Effects: []SpellEffect{{
				Kind: "damage", Dice: "1d10", DamageType: "fire",
				DiceTiers: []DiceTier{{5, "2d10"}, {11, "3d10"}, {17, "4d10"}},
			}}},
			"fireball": {ID: "fireball", Name: "Fireball", Level: 3, Save: Dex, SaveEffect: "half damage", Effects: []SpellEffect{{
				Kind: "damage", Dice: "8d6", DamageType: "fire", UpcastDice: "1d6",
			}}},
			"magic_missile": {ID: "magic_missile", Name: "Magic Missile", Level: 1},
			"cure_wounds":   {ID: "cure_wounds", Name: "Cure Wounds", Level: 1},
		},
		lists: map[string]SpellList{"wizard": wizardList},
	}
}

func TestSpellSlots(t *testing.T) {
	cases := []struct {
		name            string
		classes         []ClassLevel
		wantSlots       map[int]int
		wantCasterLevel int
	}{
		{"wizard 5", []ClassLevel{cls("wizard", 5)}, map[int]int{1: 4, 2: 3, 3: 2}, 5},
		{"paladin 5 uses its own table", []ClassLevel{cls("paladin", 5)}, map[int]int{1: 4, 2: 2}, 2},
		{"paladin 1 is not casting yet", []ClassLevel{cls("paladin", 1)}, map[int]int{}, 0},
		{"wizard 5 + paladin 4 combine", []ClassLevel{cls("wizard", 5), cls("paladin", 4)}, map[int]int{1: 4, 2: 3, 3: 3, 4: 1}, 7},
		{"warlock has no shared slots", []ClassLevel{cls("warlock", 5)}, map[int]int{}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newChar(1, nil)
			c.Classes = tc.classes
			sh := mustCompute(t, c, casterLib(), Ruleset{})
			got := slotMaxes(sh)
			if len(got) != len(tc.wantSlots) {
				t.Errorf("slots = %v, want %v", got, tc.wantSlots)
			}
			for lvl, want := range tc.wantSlots {
				if got[lvl] != want {
					t.Errorf("level %d slots = %d, want %d (all: %v)", lvl, got[lvl], want, got)
				}
			}
			if sh.CasterLevel != tc.wantCasterLevel {
				t.Errorf("caster level = %d, want %d", sh.CasterLevel, tc.wantCasterLevel)
			}
		})
	}
}

func TestPactSlots(t *testing.T) {
	c := newChar(1, nil)
	c.Classes = []ClassLevel{cls("warlock", 5)}
	sh := mustCompute(t, c, casterLib(), Ruleset{})
	if sh.PactSlots == nil || sh.PactSlots.Max != 2 || sh.PactSlots.Level != 3 {
		t.Errorf("pact slots = %+v, want 2 slots of level 3", sh.PactSlots)
	}
}

func TestSpendingTooManySlotsWarns(t *testing.T) {
	c := newChar(1, nil)
	c.Classes = []ClassLevel{cls("wizard", 3)}
	c.SpellSlotsUsed = map[int]int{1: 5} // only 4 first-level slots
	sh := mustCompute(t, c, casterLib(), Ruleset{})
	if !hasWarning(sh, WarnOverCap) {
		t.Errorf("expected an over_cap warning, got %+v", sh.Warnings)
	}
}

func wizardChar(level int, state SpellState) Character {
	c := newChar(level, map[Ability]int{Int: 16, Cha: 14})
	c.Classes = []ClassLevel{cls("wizard", level)}
	c.Spellbooks = map[string]SpellState{"class:wizard": state}
	return c
}

func TestWizardSpellComputation(t *testing.T) {
	c := wizardChar(9, SpellState{ // PB 4, Int +3
		Cantrips: []string{"fire_bolt"},
		Book:     []string{"fireball", "magic_missile"},
		Prepared: []string{"fireball"},
	})
	c.Modifiers = []Modifier{{Target: TargetDamageSpell, Kind: ModAdd, Value: 1, SourceID: "spell_sniper"}}
	sh := mustCompute(t, c, casterLib(), Ruleset{})

	src, ok := findSource(sh, "class:wizard")
	if !ok {
		t.Fatalf("no wizard source in %+v", sh.Spellcasting)
	}
	if src.SaveDC != 15 || src.AttackBonus != 7 { // 8+4+3, 4+3
		t.Errorf("DC %d / attack %d, want 15 / 7", src.SaveDC, src.AttackBonus)
	}
	if src.CantripsAllowed != 4 || src.SpellsAllowed != 12 { // table, then 3 + class level 9
		t.Errorf("allowed = %d cantrips / %d spells, want 4 / 12", src.CantripsAllowed, src.SpellsAllowed)
	}

	bolt, ok := findSpell(src.Cantrips, "fire_bolt")
	if !ok || len(bolt.Effects) != 1 {
		t.Fatalf("fire bolt missing or has no effects: %+v", src.Cantrips)
	}
	if bolt.Effects[0].Dice != "2d10" || bolt.Effects[0].Bonus != 1 || bolt.AttackBonus != 7 {
		t.Errorf("fire bolt = %+v, want 2d10 (level 5 tier), bonus 1, attack 7", bolt)
	}

	ball, ok := findSpell(src.Spells, "fireball")
	if !ok || len(ball.Effects) != 1 {
		t.Fatalf("fireball missing or has no effects: %+v", src.Spells)
	}
	if ball.SaveDC != 15 || ball.SaveAbility != Dex {
		t.Errorf("fireball save = %s DC %d, want dex 15", ball.SaveAbility, ball.SaveDC)
	}
	up := ball.Effects[0].Upcast
	if len(up) != 2 || up[0] != (UpcastEntry{4, "9d6"}) || up[1] != (UpcastEntry{5, "10d6"}) {
		t.Errorf("upcast = %+v, want 9d6 at 4 and 10d6 at 5", up)
	}
	if _, ok := findSpell(src.Spells, "magic_missile"); ok {
		t.Error("only prepared spells should be listed; magic missile is just in the book")
	}

	if p, ok := findPending(sh, "spells:class:wizard", "cantrips"); !ok || p.Remaining != 3 {
		t.Errorf("expected 3 cantrips pending, got %+v", sh.PendingChoices)
	}
	if p, ok := findPending(sh, "spells:class:wizard", "prepared"); !ok || p.Remaining != 11 {
		t.Errorf("expected 11 prepared spells pending, got %+v", sh.PendingChoices)
	}
}

func TestSpellWarnings(t *testing.T) {
	cases := []struct {
		name  string
		level int
		state SpellState
		want  WarningCode
	}{
		{"spell too high for the slots", 1, SpellState{Book: []string{"fireball"}, Prepared: []string{"fireball"}}, WarnSpellTooHigh},
		{"prepared but not in the book", 9, SpellState{Book: []string{"fireball"}, Prepared: []string{"fireball", "magic_missile"}}, WarnNotInPool},
		{"cantrip not on the list", 9, SpellState{Cantrips: []string{"cure_wounds"}}, WarnNotInPool},
		{"unknown spell id", 9, SpellState{Book: []string{"nonexistent"}, Prepared: []string{"nonexistent"}}, WarnUnknownID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sh := mustCompute(t, wizardChar(tc.level, tc.state), casterLib(), Ruleset{})
			if !hasWarning(sh, tc.want) {
				t.Errorf("expected %s, got %+v", tc.want, sh.Warnings)
			}
		})
	}
}

func TestInnateSpellSource(t *testing.T) {
	c := newChar(9, map[Ability]int{Cha: 14}) // PB 4, Cha +2
	c.FeatureIDs = []string{"fire_breath"}
	sh := mustCompute(t, c, casterLib(), Ruleset{})
	src, ok := findSource(sh, "feature:fire_breath")
	if !ok {
		t.Fatalf("no innate source in %+v", sh.Spellcasting)
	}
	if src.SaveDC != 14 || src.Prep != "innate" { // 8+4+2
		t.Errorf("innate source = DC %d, prep %q; want 14 and innate", src.SaveDC, src.Prep)
	}
	if _, ok := findSpell(src.Cantrips, "fire_bolt"); !ok {
		t.Error("fire bolt should be listed as a cantrip")
	}
}

func TestUnknownConcentrationSpellWarns(t *testing.T) {
	c := newChar(1, nil)
	c.Concentrating = "nope"
	sh := mustCompute(t, c, fakeLib{}, Ruleset{})
	if !hasWarningValue(sh, WarnUnknownID, "nope") {
		t.Errorf("expected unknown_id, got %+v", sh.Warnings)
	}
}

// ---------------------------------------------------------------------------
// Warnings: caps and acknowledgements
// ---------------------------------------------------------------------------

func TestInspirationCapAndAcknowledgement(t *testing.T) {
	c := newChar(1, nil)
	c.Inspiration = 5
	rules := Ruleset{MaxInspiration: 3}

	sh := mustCompute(t, c, fakeLib{}, rules)
	w, ok := findWarning(sh, WarnOverCap)
	if !ok || w.Acknowledged {
		t.Fatalf("expected an unacknowledged over_cap warning, got %+v", sh.Warnings)
	}

	c.Acknowledgements = []Acknowledgement{{Key: WarningKey(WarnOverCap, "", "", "5"), Note: "DM approved"}}
	sh = mustCompute(t, c, fakeLib{}, rules)
	w, _ = findWarning(sh, WarnOverCap)
	if !w.Acknowledged || w.AckNote != "DM approved" {
		t.Errorf("warning = %+v, want acknowledged with note", w)
	}

	// Change the value and the old acknowledgement lapses.
	c.Inspiration = 4
	sh = mustCompute(t, c, fakeLib{}, rules)
	w, _ = findWarning(sh, WarnOverCap)
	if w.Acknowledged {
		t.Error("acknowledgement should lapse when the offending value changes")
	}
}
