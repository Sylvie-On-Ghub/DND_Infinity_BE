package character

// forms_test.go - tests for forms (Rage, Bladesong): modifiers that count while a form is on and show as
// "while <form> is active" notes while it's off, plus Sheet.Forms and the warnings for stale stored state.

import (
	"reflect"
	"slices"
	"testing"
)

func formsLib() fakeLib {
	return fakeLib{features: map[string]Feature{
		"rager": {ID: "rager", Name: "Rager",
			Form:     &FormDef{ID: "rage", Name: "Rage"},
			Resource: &Resource{Name: "Rage", Max: 2, Recharge: RechargeLongRest},
			Modifiers: []Modifier{
				{Target: "resist.fire", Kind: ModAdd, Value: 1, Form: "rage"},
				{Target: "save.str", Kind: ModAdvantage, Form: "rage", Condition: "vs. grapples"},
			}},
		"wild": {ID: "wild", Name: "Wild", FormChoices: []FormChoice{{Form: "rage", Choice: Choice{
			ID: "animal", Kind: ChoicePick, Count: 1, Options: []Option{
				{ID: "bear", Modifiers: []Modifier{{Target: "resist.cold", Kind: ModAdd, Value: 1}}},
				{ID: "wolf", Modifiers: []Modifier{{Target: "x", Kind: ModNote, Note: "Wolf"}}},
			}}}}},
	}}
}

// formsChar has no class, so the only warnings are the ones forms raise.
func formsChar(featureIDs ...string) Character {
	return Character{ID: "c", Name: "Test", FeatureIDs: featureIDs}
}

func findNote(sh Sheet, target string) (Modifier, bool) {
	for _, m := range sh.Notes {
		if m.Target == target {
			return m, true
		}
	}
	return Modifier{}, false
}

func TestFormsOff(t *testing.T) {
	sh := mustCompute(t, formsChar("rager", "wild"), formsLib(), Ruleset{})

	if slices.Contains(sh.Resistances, "fire") {
		t.Errorf("fire resistance should only apply while raging, got %v", sh.Resistances)
	}
	if n, ok := findNote(sh, "resist.fire"); !ok || n.Condition != "while Rage is active" {
		t.Errorf("resist.fire note = %+v (found %v), want condition %q", n, ok, "while Rage is active")
	}
	if n, ok := findNote(sh, "save.str"); !ok || n.Condition != "while Rage is active, vs. grapples" {
		t.Errorf("save.str note = %+v (found %v), want condition %q", n, ok, "while Rage is active, vs. grapples")
	}
	if len(sh.Forms) != 1 {
		t.Fatalf("Forms = %+v, want one", sh.Forms)
	}
	f := sh.Forms[0]
	if f.ID != "rage" || f.Name != "Rage" || f.FeatureID != "rager" || f.Active || f.ResourceID != "rager" || f.UsesLeft != 2 {
		t.Errorf("form = %+v", f)
	}
	if len(f.Choices) != 1 || f.Choices[0].ID != "animal" {
		t.Errorf("form choices = %+v, want animal", f.Choices)
	}
}

func TestFormsOn(t *testing.T) {
	c := formsChar("rager", "wild")
	c.ActiveForms = []ActiveForm{{ID: "rage", Picks: map[string][]string{"animal": {"bear"}}}}
	sh := mustCompute(t, c, formsLib(), Ruleset{})

	if !slices.Contains(sh.Resistances, "fire") || !slices.Contains(sh.Resistances, "cold") {
		t.Errorf("Resistances = %v, want fire and cold", sh.Resistances)
	}
	if n, ok := findNote(sh, "save.str"); !ok || n.Condition != "vs. grapples" {
		t.Errorf("save.str note = %+v (found %v), want condition %q", n, ok, "vs. grapples")
	}
	if len(sh.Forms) != 1 || !sh.Forms[0].Active {
		t.Fatalf("Forms = %+v, want rage active", sh.Forms)
	}
	if want := map[string][]string{"animal": {"bear"}}; !reflect.DeepEqual(sh.Forms[0].Picks, want) {
		t.Errorf("Picks = %v, want %v", sh.Forms[0].Picks, want)
	}
	if len(sh.Warnings) != 0 {
		t.Errorf("Warnings = %+v, want none", sh.Warnings)
	}
}

func TestFormsResourceFromTier(t *testing.T) {
	lib := fakeLib{features: map[string]Feature{
		"blade": {ID: "blade", Name: "Bladesinger", Form: &FormDef{ID: "bladesong", Name: "Bladesong"},
			Tiers: []Tier{{MinLevel: 1, Resource: &Resource{Name: "Bladesong", Max: 3, Recharge: RechargeLongRest}}}},
	}}
	c := newChar(1, nil)
	c.FeatureIDs = []string{"blade"}
	sh := mustCompute(t, c, lib, Ruleset{})

	if len(sh.Forms) != 1 || sh.Forms[0].ResourceID != "blade" || sh.Forms[0].UsesLeft != 3 {
		t.Errorf("Forms = %+v, want resource blade with 3 uses", sh.Forms)
	}
}

func TestFormsNoCost(t *testing.T) {
	lib := fakeLib{features: map[string]Feature{
		"free": {ID: "free", Name: "Free", Form: &FormDef{ID: "free_form", Name: "Free Form", NoCost: true},
			Resource: &Resource{Name: "Free", Max: 1, Recharge: RechargeLongRest}},
	}}
	sh := mustCompute(t, formsChar("free"), lib, Ruleset{})

	if len(sh.Forms) != 1 || sh.Forms[0].ResourceID != "" {
		t.Errorf("Forms = %+v, want no resource", sh.Forms)
	}
}

func TestFormsUnavailable(t *testing.T) {
	c := formsChar("wild")
	c.ActiveForms = []ActiveForm{{ID: "rage"}}
	c.Modifiers = []Modifier{{Target: "resist.fire", Kind: ModAdd, Value: 1, Form: "rage"}}
	sh := mustCompute(t, c, formsLib(), Ruleset{})

	if !hasWarning(sh, WarnFormUnavailable) {
		t.Errorf("want %s, got %+v", WarnFormUnavailable, sh.Warnings)
	}
	if slices.Contains(sh.Resistances, "fire") {
		t.Errorf("an unavailable form shouldn't count, got %v", sh.Resistances)
	}
}

func TestFormsPickWarnings(t *testing.T) {
	cases := []struct {
		name  string
		picks map[string][]string
		want  WarningCode
	}{
		{"missing", nil, WarnFormPickMissing},
		{"unknown option", map[string][]string{"animal": {"tiger"}}, WarnFormPickInvalid},
		{"over count", map[string][]string{"animal": {"bear", "wolf"}}, WarnFormPickInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := formsChar("rager", "wild")
			c.ActiveForms = []ActiveForm{{ID: "rage", Picks: tc.picks}}
			sh := mustCompute(t, c, formsLib(), Ruleset{})

			w, ok := findWarning(sh, tc.want)
			if !ok {
				t.Fatalf("want %s, got %+v", tc.want, sh.Warnings)
			}
			if w.FeatureID != "wild" || w.ChoiceID != "animal" {
				t.Errorf("warning = %+v, want feature wild, choice animal", w)
			}
			if tc.name == "unknown option" && slices.Contains(sh.Resistances, "cold") {
				t.Errorf("no valid pick, so no cold resistance, got %v", sh.Resistances)
			}
			if tc.name == "over count" && !slices.Contains(sh.Resistances, "cold") {
				t.Errorf("the pick within the count should still apply, got %v", sh.Resistances)
			}
		})
	}
}

func TestFormsDuplicatePick(t *testing.T) {
	lib := formsLib()
	wild := lib.features["wild"]
	wild.FormChoices[0].Choice.Count = 2
	c := formsChar("rager", "wild")
	c.ActiveForms = []ActiveForm{{ID: "rage", Picks: map[string][]string{"animal": {"wolf", "wolf"}}}}
	sh := mustCompute(t, c, lib, Ruleset{})

	if !hasWarning(sh, WarnFormPickInvalid) {
		t.Errorf("want %s, got %+v", WarnFormPickInvalid, sh.Warnings)
	}
	notes := 0
	for _, n := range sh.Notes {
		if n.Note == "Wolf" {
			notes++
		}
	}
	if notes != 1 {
		t.Errorf("the wolf note applied %d times, want once: %+v", notes, sh.Notes)
	}
}

func TestFormsStalePicks(t *testing.T) {
	c := formsChar("rager")
	c.ActiveForms = []ActiveForm{{ID: "rage", Picks: map[string][]string{"animal": {"bear"}}}}
	sh := mustCompute(t, c, formsLib(), Ruleset{})

	if !hasWarning(sh, WarnFormPickInvalid) {
		t.Errorf("want %s, got %+v", WarnFormPickInvalid, sh.Warnings)
	}
	if hasWarning(sh, WarnFormPickMissing) {
		t.Errorf("no choice is offered, so nothing is missing: %+v", sh.Warnings)
	}
	if slices.Contains(sh.Resistances, "cold") {
		t.Errorf("a stale pick shouldn't count, got %v", sh.Resistances)
	}
}

func TestFormsAdHocUnknown(t *testing.T) {
	c := formsChar()
	base := mustCompute(t, c, formsLib(), Ruleset{})
	c.Modifiers = []Modifier{{Target: "ac", Kind: ModAdd, Value: 2, Form: "nope"}}
	sh := mustCompute(t, c, formsLib(), Ruleset{})

	if w, ok := findWarning(sh, WarnFormUnknown); !ok || w.Value != "nope" {
		t.Errorf("want %s for nope, got %+v", WarnFormUnknown, sh.Warnings)
	}
	if sh.AC != base.AC {
		t.Errorf("AC = %d, want %d", sh.AC, base.AC)
	}
	if n, ok := findNote(sh, "ac"); !ok || n.Condition != "while nope is active" {
		t.Errorf("ac note = %+v (found %v), want condition %q", n, ok, "while nope is active")
	}
}

func TestStrengthAttackRequirement(t *testing.T) {
	lib := weaponLib()
	lib.items["str_sword"] = Item{ID: "str_sword", Name: "Str Sword", Kind: ItemWeapon, Weapon: &WeaponStats{
		Category: "martial", Attack: AttackDetails{DamageDice: "1d8", DamageType: "slashing", RangeNormal: 5, Ability: Str},
	}}
	c := newChar(1, map[Ability]int{Str: 10, Dex: 10})
	c.Modifiers = []Modifier{{Target: TargetDamageAll, Kind: ModAdd, Value: 2, Requires: RequiresStrengthAttack, SourceID: "rage"}}
	equip(&c, "str_sword", "rapier")
	c.Inventory[1].AbilityOverride = Dex
	sh := mustCompute(t, c, lib, Ruleset{})

	for name, want := range map[string]int{"Str Sword": 2, "Rapier": 0, "Unarmed Strike": 3} { // unarmed deals 1 flat
		a, ok := findAttack(sh, name)
		if !ok || a.DamageBonus != want {
			t.Errorf("%s damage bonus = %d (found %v), want %d", name, a.DamageBonus, ok, want)
		}
	}
}

func TestOnlyCasterRequirement(t *testing.T) {
	lib := fakeLib{classes: map[string]Class{
		"sorcerer": {ID: "sorcerer", Name: "Sorcerer", Spellcasting: &SpellcastingDef{Ability: Cha, Divisor: 1, Prep: PrepKnown}},
		"wizard":   {ID: "wizard", Name: "Wizard", Spellcasting: &SpellcastingDef{Ability: Int, Divisor: 1, Prep: PrepPrepared}},
	}}
	mod := Modifier{Target: TargetSpellSaveDC, Kind: ModAdd, Value: 1, Requires: RequiresOnlyCasterPrefix + "sorcerer", SourceID: "innate"}
	dc := func(classes ...ClassLevel) (int, Sheet) {
		c := Character{ID: "c", Name: "T", Classes: classes, Modifiers: []Modifier{mod},
			Abilities: map[Ability]AbilityScore{Cha: {Score: 14}, Int: {Score: 14}}}
		sh := mustCompute(t, c, lib, Ruleset{})
		src, ok := findSource(sh, "class:sorcerer")
		if !ok {
			t.Fatalf("no sorcerer source in %+v", sh.Spellcasting)
		}
		return src.SaveDC, sh
	}

	if got, sh := dc(cls("sorcerer", 1)); got != 8+2+2+1 {
		t.Errorf("single-class DC = %d, want 13", got)
	} else if _, ok := findNote(sh, TargetSpellSaveDC); ok {
		t.Error("a met requirement shouldn't leave a note")
	}

	got, sh := dc(cls("sorcerer", 1), cls("wizard", 1))
	if got != 8+2+2 {
		t.Errorf("multiclass DC = %d, want 12", got)
	}
	n, ok := findNote(sh, TargetSpellSaveDC)
	if !ok || n.Condition != "with Sorcerer spells" {
		t.Errorf("note = %+v (found %v), want condition %q", n, ok, "with Sorcerer spells")
	}
}

func TestFormsPickSourcesDontDedupe(t *testing.T) {
	lib := formsLib()
	lib.features["wild"] = Feature{ID: "wild", Name: "Wild", FormChoices: []FormChoice{{Form: "rage", Choice: Choice{
		ID: "animal", Kind: ChoicePick, Count: 2, Options: []Option{
			{ID: "bear", Modifiers: []Modifier{{Target: "speed.walk", Kind: ModAdd, Value: 5}}},
			{ID: "wolf", Modifiers: []Modifier{{Target: "speed.walk", Kind: ModAdd, Value: 5}}},
		}}}}}
	base := mustCompute(t, formsChar("rager", "wild"), lib, Ruleset{})
	c := formsChar("rager", "wild")
	c.ActiveForms = []ActiveForm{{ID: "rage", Picks: map[string][]string{"animal": {"bear", "wolf"}}}}
	sh := mustCompute(t, c, lib, Ruleset{})

	if sh.Speeds[SpeedWalk] != base.Speeds[SpeedWalk]+10 {
		t.Errorf("walk speed = %d, want %d: both picks should apply", sh.Speeds[SpeedWalk], base.Speeds[SpeedWalk]+10)
	}
}

func TestFormsDuplicateActiveEntries(t *testing.T) {
	c := formsChar("rager", "wild")
	c.ActiveForms = []ActiveForm{
		{ID: "rage", Picks: map[string][]string{"animal": {"bear"}}},
		{ID: "rage", Picks: map[string][]string{"animal": {"nope"}}},
		{ID: "gone"},
		{ID: "gone"},
	}
	sh := mustCompute(t, c, formsLib(), Ruleset{})

	unavailable := 0
	for _, w := range sh.Warnings {
		if w.Code == WarnFormUnavailable {
			unavailable++
		}
		if w.Code == WarnFormPickInvalid {
			t.Errorf("the second rage entry should be ignored, got %+v", w)
		}
	}
	if unavailable != 1 {
		t.Errorf("%d %s warnings, want 1: %+v", unavailable, WarnFormUnavailable, sh.Warnings)
	}
	if len(sh.Forms) != 1 || !reflect.DeepEqual(sh.Forms[0].Picks["animal"], []string{"bear"}) {
		t.Errorf("forms = %+v, want the first entry's picks", sh.Forms)
	}
}
