package character

// shape_test.go - tests for Wild Shape: Known Forms, the shape info on FormView and the shaped sheet.
// shapeLib and druidChar are the fixture shared by the other shape tests.

import (
	"slices"
	"strings"
	"testing"
)

func shapeLib() fakeLib {
	levels := make([][]string, 20)
	levels[1] = []string{"wild"}
	wolfAttack := CreatureAttack{Name: "Bite", Type: ActionAction, Reach: 5,
		ToHit:      Formula{Terms: []Term{{Source: "mod.str"}, {Source: "pb"}}},
		DamageDice: "1d6", Damage: Formula{Terms: []Term{{Source: "mod.str"}}}, DamageType: "piercing"}
	bearAttack := CreatureAttack{Name: "Rend", Type: ActionAction, Reach: 5,
		ToHit:      Formula{Terms: []Term{{Source: "mod.str"}, {Source: "pb"}}},
		DamageDice: "1d8", Damage: Formula{Terms: []Term{{Source: "mod.str"}}}, DamageType: "slashing"}
	tag := []string{"wild_shape"}
	return fakeLib{
		classes: map[string]Class{"druid": {ID: "druid", Name: "Druid", HitDie: 8,
			StartingGrants: Grants{Saves: []Ability{Int, Wis}}, Levels: levels}},
		features: map[string]Feature{
			"wild": {ID: "wild", Name: "Wild Shape",
				Form: &FormDef{ID: "wild_shape", Name: "Wild Shape", Shape: &ShapeDef{
					CreatureTag: "wild_shape", ClassID: "druid",
					Limits: []ShapeLimit{{2, 4, "1/4", false}, {4, 6, "1/2", false}, {8, 8, "1", true}},
					TempHP: Formula{Terms: []Term{{Source: "class_level.druid"}}},
				}},
				Resource: &Resource{Name: "Wild Shape", Max: 2, Recharge: RechargeLongRest, ShortRestRegain: 1}},
			"moon": {ID: "moon", Name: "Circle of the Moon",
				ACFormulas: []ACFormula{{ID: "circle_forms", Name: "Circle Forms",
					Formula: Formula{Base: 13, Terms: []Term{{Source: "mod.wis"}}}, Form: "wild_shape"}},
				Modifiers: []Modifier{
					{Target: TargetShapeMaxCR, Kind: ModMin, Form: "wild_shape", ValueFormula: &Formula{Terms: []Term{{Source: "class_level.druid", Div: 3}}}},
					{Target: TargetShapeTempHP, Kind: ModSet, Form: "wild_shape", ValueFormula: &Formula{Terms: []Term{{Source: "class_level.druid", Mul: 3}}}},
					{Target: "save.con", Kind: ModAdd, Form: "wild_shape", ValueAbility: Wis},
				}},
		},
		creatures: map[string]Creature{
			"wolf": {ID: "wolf", Name: "Wolf", Size: "Medium", Tags: tag, CR: "1/4",
				Abilities: map[Ability]int{Str: 14, Dex: 15, Con: 12, Wis: 12},
				AC:        Formula{Base: 12}, Speeds: map[SpeedType]int{SpeedWalk: 40},
				Skills:  map[string]ProficiencyLevel{"perception": ProfExpert, "stealth": ProfTrained},
				Senses:  []Sense{{Name: "darkvision", Range: 60}},
				Attacks: []CreatureAttack{wolfAttack}, Traits: []Trait{{Name: "Pack Tactics"}}},
			"bear": {ID: "bear", Name: "Bear", Size: "Large", Tags: tag, CR: "1",
				Abilities: map[Ability]int{Str: 19, Dex: 10, Con: 16},
				AC:        Formula{Base: 11}, Speeds: map[SpeedType]int{SpeedWalk: 40, SpeedClimb: 30},
				Saves: []Ability{Con}, Attacks: []CreatureAttack{bearAttack}},
			"hawk": {ID: "hawk", Name: "Hawk", Size: "Tiny", Tags: tag, CR: "0",
				Abilities: map[Ability]int{Str: 5, Dex: 16, Con: 8},
				AC:        Formula{Base: 13}, Speeds: map[SpeedType]int{SpeedWalk: 10, SpeedFly: 60}},
			"shell": {ID: "shell", Name: "Shell", Size: "Medium", Tags: tag, CR: "1/2",
				Abilities: map[Ability]int{Str: 10, Dex: 10, Con: 14},
				AC:        Formula{Base: 17}, Speeds: map[SpeedType]int{SpeedWalk: 20}},
			"croc": {ID: "croc", Name: "Croc", Size: "Large", Tags: tag, CR: "2",
				Abilities: map[Ability]int{Str: 15, Dex: 10, Con: 13},
				AC:        Formula{Base: 12}, Speeds: map[SpeedType]int{SpeedWalk: 20, SpeedSwim: 30}},
			"imp": {ID: "imp", Name: "Imp", Size: "Tiny", CR: "1",
				Abilities: map[Ability]int{Str: 6, Dex: 17, Con: 13},
				AC:        Formula{Base: 13}, Speeds: map[SpeedType]int{SpeedWalk: 20}},
		},
	}
}

func druidChar(level int, known ...string) Character {
	c := Character{ID: "c", Name: "Test", FeatureIDs: []string{},
		Abilities: map[Ability]AbilityScore{
			Str: {Score: 10}, Dex: {Score: 12}, Con: {Score: 14},
			Int: {Score: 10}, Wis: {Score: 16}, Cha: {Score: 10}},
		Classes: []ClassLevel{cls("druid", level)},
		Skills:  map[string]ProficiencyLevel{"stealth": ProfExpert}}
	if len(known) > 0 {
		c.Selections = []Selection{{FeatureID: "wild", ChoiceID: KnownFormsChoice, Picked: known}}
	}
	return c
}

func pendingKnown(sh Sheet) (PendingChoice, bool) {
	for _, p := range sh.PendingChoices {
		if p.ChoiceID == KnownFormsChoice {
			return p, true
		}
	}
	return PendingChoice{}, false
}

func shapeForm(t *testing.T, sh Sheet) FormView {
	t.Helper()
	for _, f := range sh.Forms {
		if f.ID == "wild_shape" {
			return f
		}
	}
	t.Fatalf("no wild_shape form in %+v", sh.Forms)
	return FormView{}
}

func TestKnownFormsPending(t *testing.T) {
	for _, tc := range []struct {
		level  int
		known  []string
		remain int
	}{{2, nil, 4}, {4, nil, 6}, {8, []string{"wolf", "bear"}, 6}} {
		sh := mustCompute(t, druidChar(tc.level, tc.known...), shapeLib(), Ruleset{})
		p, ok := pendingKnown(sh)
		if !ok || p.FeatureID != "wild" || p.Remaining != tc.remain {
			t.Errorf("level %d: pending = %+v (found %v), want wild known_forms remaining %d", tc.level, p, ok, tc.remain)
		}
	}
	sh := mustCompute(t, druidChar(1), shapeLib(), Ruleset{})
	if p, ok := pendingKnown(sh); ok {
		t.Errorf("level 1: unexpected pending %+v", p)
	}
}

func TestKnownFormsView(t *testing.T) {
	picks := []string{"wolf", "bear", "hawk", "shell"}
	f := shapeForm(t, mustCompute(t, druidChar(4, picks...), shapeLib(), Ruleset{}))
	if f.MaxCR != "1/2" || f.FlyAllowed || f.TempHP != 4 {
		t.Errorf("level 4: MaxCR %q Fly %v TempHP %d, want 1/2 false 4", f.MaxCR, f.FlyAllowed, f.TempHP)
	}
	want := []ShapeOption{
		{ID: "wolf", Name: "Wolf", CR: "1/4", Eligible: true},
		{ID: "bear", Name: "Bear", CR: "1", Reason: "CR 1 is above your max of 1/2"},
		{ID: "hawk", Name: "Hawk", CR: "0", Reason: "has a fly speed, which Wild Shape allows from level 8"},
		{ID: "shell", Name: "Shell", CR: "1/2", Eligible: true},
	}
	if !slices.Equal(f.KnownForms, want) {
		t.Errorf("level 4 KnownForms = %+v, want %+v", f.KnownForms, want)
	}

	f = shapeForm(t, mustCompute(t, druidChar(8, picks...), shapeLib(), Ruleset{}))
	if f.MaxCR != "1" || !f.FlyAllowed {
		t.Errorf("level 8: MaxCR %q Fly %v, want 1 true", f.MaxCR, f.FlyAllowed)
	}
	for _, o := range f.KnownForms {
		if !o.Eligible {
			t.Errorf("level 8: %+v should be eligible", o)
		}
	}
}

func TestKnownFormsMoon(t *testing.T) {
	moon := func(level int) FormView {
		c := druidChar(level)
		c.FeatureIDs = []string{"moon"}
		return shapeForm(t, mustCompute(t, c, shapeLib(), Ruleset{}))
	}
	if f := moon(3); f.MaxCR != "1" || f.TempHP != 9 {
		t.Errorf("level 3: MaxCR %q TempHP %d, want 1 9", f.MaxCR, f.TempHP)
	}
	if f := moon(9); f.MaxCR != "3" {
		t.Errorf("level 9: MaxCR %q, want 3", f.MaxCR)
	}
	if f := moon(4); f.TempHP != 12 {
		t.Errorf("level 4: TempHP %d, want 12", f.TempHP)
	}
}

func TestKnownFormsPool(t *testing.T) {
	sh := mustCompute(t, druidChar(4, "imp"), shapeLib(), Ruleset{})
	found := false
	for _, w := range sh.Warnings {
		if w.Code == WarnNotInPool && w.Value == "imp" {
			found = true
		}
	}
	if !found {
		t.Errorf("want a not_in_pool warning for imp, got %+v", sh.Warnings)
	}

	opts, err := ChoiceOptions(druidChar(4), shapeLib(), Ruleset{}, "wild", KnownFormsChoice)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, o := range opts {
		ids = append(ids, o.OptionID)
		if o.OptionID == "croc" && (o.Eligible || o.Reason != "CR 2 is above your max of 1/2") {
			t.Errorf("croc = %+v, want ineligible for CR", o)
		}
	}
	if want := []string{"bear", "croc", "hawk", "shell", "wolf"}; !slices.Equal(ids, want) {
		t.Errorf("options = %v, want %v", ids, want)
	}
}

// shaped computes a druid shaped as creatureID, with these features and ad hoc modifiers.
func shaped(t *testing.T, c Character, creatureID string, featureIDs []string, mods ...Modifier) Sheet {
	t.Helper()
	c.ActiveForms = []ActiveForm{{ID: "wild_shape", CreatureID: creatureID}}
	c.FeatureIDs = append(c.FeatureIDs, featureIDs...)
	c.Modifiers = append(c.Modifiers, mods...)
	return mustCompute(t, c, shapeLib(), Ruleset{})
}

func TestShapeOverlay(t *testing.T) {
	c := druidChar(4, "wolf", "bear", "hawk", "shell")
	dmg := Modifier{Target: TargetDamageAll, Kind: ModAdd, Value: 1, SourceID: "adhoc"}
	unshaped := mustCompute(t, c, shapeLib(), Ruleset{})
	sh := shaped(t, c, "wolf", nil, dmg)

	ab := sh.Abilities
	if ab[Str].Score != 14 || ab[Dex].Score != 15 || ab[Con].Score != 12 || ab[Wis].Score != 16 {
		t.Errorf("scores Str %d Dex %d Con %d Wis %d, want 14 15 12 16", ab[Str].Score, ab[Dex].Score, ab[Con].Score, ab[Wis].Score)
	}
	if ab[Wis].SaveBonus != 5 || ab[Dex].SaveBonus != 2 {
		t.Errorf("saves Wis %d Dex %d, want 5 2", ab[Wis].SaveBonus, ab[Dex].SaveBonus)
	}
	for id, want := range map[string]int{"stealth": 6, "perception": 5, "athletics": 2} {
		if got := sh.Skills[id].Bonus; got != want {
			t.Errorf("skill %s = %d, want %d", id, got, want)
		}
	}
	if sh.Initiative != 2 || sh.AC != 12 || sh.ACDetail.FormulaID != "shape" || sh.ACDetail.Formula != "Wolf natural armour" {
		t.Errorf("init %d AC %d (%q %q), want 2 12 (shape, Wolf natural armour)", sh.Initiative, sh.AC, sh.ACDetail.FormulaID, sh.ACDetail.Formula)
	}
	if sh.MaxHP != unshaped.MaxHP {
		t.Errorf("MaxHP = %d, want %d (unchanged)", sh.MaxHP, unshaped.MaxHP)
	}
	if len(sh.Speeds) != 1 || sh.Speeds[SpeedWalk] != 40 {
		t.Errorf("Speeds = %v, want walk 40 only", sh.Speeds)
	}
	if !slices.Contains(sh.Senses, Sense{Name: "darkvision", Range: 60}) {
		t.Errorf("Senses = %v, want darkvision 60", sh.Senses)
	}
	if _, ok := findAttack(sh, "Unarmed Strike"); ok {
		t.Errorf("Unarmed Strike should be replaced while shaped: %+v", sh.Attacks)
	}
	bite, ok := findAttack(sh, "Bite")
	if !ok || bite.ToHit != 4 || bite.DamageDice != "1d6" || bite.DamageBonus != 3 || bite.DamageType != "piercing" || bite.Ability != Str {
		t.Errorf("Bite = %+v (found %v), want +4, 1d6+3 piercing, str", bite, ok)
	}
	want := ShapeView{FormID: "wild_shape", CreatureID: "wolf", Name: "Wolf", Size: "Medium", CR: "1/4", Traits: []Trait{{Name: "Pack Tactics"}}}
	if s := sh.Shape; s == nil || s.FormID != want.FormID || s.CreatureID != want.CreatureID || s.Name != want.Name ||
		s.Size != want.Size || s.CR != want.CR || !slices.Equal(s.Traits, want.Traits) || s.CanCast {
		t.Errorf("Shape = %+v, want %+v", s, want)
	}
}

func TestShapeBlockSaveWins(t *testing.T) {
	sh := shaped(t, druidChar(8, "bear"), "bear", nil)
	if got := sh.Abilities[Con].SaveBonus; got != 5 {
		t.Errorf("Con save = %d, want 5 (bear's 3 + 2 beats own 3)", got)
	}
}

func TestShapeOff(t *testing.T) {
	sh := mustCompute(t, druidChar(4, "wolf", "bear", "hawk", "shell"), shapeLib(), Ruleset{})
	if sh.Abilities[Str].Score != 10 || sh.Shape != nil {
		t.Errorf("Str %d Shape %+v, want 10 and nil", sh.Abilities[Str].Score, sh.Shape)
	}
	if _, ok := findAttack(sh, "Unarmed Strike"); !ok {
		t.Errorf("want Unarmed Strike unshaped, got %+v", sh.Attacks)
	}
}

func TestShapeCircleForms(t *testing.T) {
	moon := []string{"moon"}
	sh := shaped(t, druidChar(4, "wolf", "shell"), "wolf", moon)
	if sh.AC != 16 || sh.ACDetail.FormulaID != "circle_forms" {
		t.Errorf("wolf AC %d (%s), want 16 circle_forms", sh.AC, sh.ACDetail.FormulaID)
	}
	if got := sh.Abilities[Con].SaveBonus; got != 4 {
		t.Errorf("wolf Con save = %d, want 4 (1 + Wis 3)", got)
	}
	sh = shaped(t, druidChar(4, "wolf", "shell"), "shell", moon)
	if sh.AC != 17 || sh.ACDetail.FormulaID != "shape" {
		t.Errorf("shell AC %d (%s), want 17 shape", sh.AC, sh.ACDetail.FormulaID)
	}
}

func TestShapeStrengthAttack(t *testing.T) {
	m := Modifier{Target: TargetDamageMelee, Kind: ModAdd, Value: 2, Requires: RequiresStrengthAttack, SourceID: "rage"}
	sh := shaped(t, druidChar(4, "wolf"), "wolf", nil, m)
	if bite, _ := findAttack(sh, "Bite"); bite.DamageBonus != 4 {
		t.Errorf("Bite damage bonus = %d, want 4 (Str 2 + 2)", bite.DamageBonus)
	}
}

func TestShapeArmourIgnored(t *testing.T) {
	lib := shapeLib()
	lib.items = map[string]Item{"plate": {ID: "plate", Name: "Plate", Kind: ItemArmour,
		ACFormula: &ACFormula{ID: "plate", Name: "Plate", Formula: Formula{Base: 18}}}}
	c := druidChar(4, "wolf")
	c.Inventory = []InventoryItem{{ItemID: "plate", Quantity: 1, Equipped: true}}
	c.ActiveForms = []ActiveForm{{ID: "wild_shape", CreatureID: "wolf"}}
	sh := mustCompute(t, c, lib, Ruleset{})
	if sh.AC != 12 || sh.ACDetail.FormulaID != "shape" {
		t.Errorf("AC %d (%s), want 12 shape: armour doesn't count while shaped", sh.AC, sh.ACDetail.FormulaID)
	}
}

func TestShapeWarnings(t *testing.T) {
	for _, tc := range []struct {
		name     string
		c        Character
		creature string
		code     WarningCode
		str      int
	}{
		{"unknown creature", druidChar(4, "wolf"), "ghost", WarnShapeUnknownCreature, 10},
		{"no creature", druidChar(4, "wolf"), "", WarnShapeUnknownCreature, 10},
		{"not known", druidChar(4, "shell"), "wolf", WarnShapeNotKnown, 14},
		{"ineligible", druidChar(4, "bear"), "bear", WarnShapeIneligible, 19},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sh := shaped(t, tc.c, tc.creature, nil)
			var codes []WarningCode
			for _, w := range sh.Warnings {
				if strings.HasPrefix(string(w.Code), "shape_") {
					codes = append(codes, w.Code)
				}
			}
			if !slices.Equal(codes, []WarningCode{tc.code}) {
				t.Errorf("shape warnings = %v, want [%s]", codes, tc.code)
			}
			if got := sh.Abilities[Str].Score; got != tc.str {
				t.Errorf("Str = %d, want %d", got, tc.str)
			}
			if (sh.Shape != nil) != (tc.code != WarnShapeUnknownCreature) {
				t.Errorf("Shape = %+v, want overlay only when the creature is found", sh.Shape)
			}
		})
	}
}
