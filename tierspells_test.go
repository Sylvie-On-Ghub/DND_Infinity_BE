package character

// tierspells_test.go - tests for level-gated spell grants: a feature tier can carry a spells block.
// Written without being able to run them: a failure may be a test mistake as much as a code bug.

import (
	"sort"
	"testing"
)

func tierSpellsLib(t *testing.T) *ContentLibrary {
	t.Helper()
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"spells/s.json": `[
			{"id":"a","name":"A","level":0},
			{"id":"b","name":"B","level":1},
			{"id":"c","name":"C","level":2}]`,
		"features/f.json": `[
			{"id":"gift","name":"Gift","origin":"race",
			 "spells":{"spell_ids":["a"],"ability":"cha"},
			 "tiers":[
			  {"min_level":3,"spells":{"spell_ids":["b"]}},
			  {"min_level":5,"spells":{"spell_ids":["c"],"ability":"int",
			    "resource":{"name":"Gift","max":1,"recharge":"long_rest"}}}]},
			{"id":"later","name":"Later","origin":"race",
			 "tiers":[{"min_level":3,"spells":{"spell_ids":["b"],"ability":"wis"}}]},
			{"id":"classgift","name":"Class Gift","origin":"class",
			 "tiers":[{"min_level":3,"spells":{"spell_ids":["b"],"ability":"cha"}}]}]`,
		"classes/c.json": `[
			{"id":"caster","name":"Caster","hit_die":8,"levels":[["classgift"],[],[],[],[]]},
			{"id":"plain","name":"Plain","hit_die":8,"levels":[[],[],[],[],[]]}]`,
	}))
	if lib.HasErrors() {
		dumpIssues(t, lib)
		t.Fatal("the test pack should load without errors")
	}
	return lib
}

func tierChar(featureID string, classes ...ClassLevel) Character {
	return Character{
		ID: "t", Name: "T", FeatureIDs: []string{featureID}, Classes: classes,
		Abilities: map[Ability]AbilityScore{Int: {Score: 14}, Cha: {Score: 10}, Wis: {Score: 16}},
	}
}

func sourceSpellIDs(src SpellSourceView) []string {
	var ids []string
	for _, s := range src.Cantrips {
		ids = append(ids, s.ID)
	}
	for _, s := range src.Spells {
		ids = append(ids, s.ID)
	}
	sort.Strings(ids)
	return ids
}

func sameIDs(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestTierSpellsAddToTheFeaturesOwn(t *testing.T) {
	lib := tierSpellsLib(t)
	at := func(level int) Sheet {
		return mustCompute(t, tierChar("gift", cls("plain", level)), lib, Ruleset{})
	}

	// Below the first tier: just the feature's own cantrip, on Charisma.
	src, ok := findSource(at(2), "feature:gift")
	if !ok || !sameIDs(sourceSpellIDs(src), "a") || src.Ability != Cha || src.SaveDC != 10 { // 8 + PB 2 + 0
		t.Errorf("level 2: %+v, want only spell a on Charisma with DC 10", src)
	}

	// Tier 3 adds a spell and inherits the ability and (lack of a) resource.
	three := at(3)
	src, ok = findSource(three, "feature:gift")
	if !ok || !sameIDs(sourceSpellIDs(src), "a", "b") || src.Ability != Cha {
		t.Errorf("level 3: %+v, want spells a and b on Charisma", src)
	}
	for _, r := range three.Resources {
		if r.FeatureID == "feature:gift" {
			t.Errorf("level 3 has no resource yet, got %+v", r)
		}
	}

	// Tier 5 replaces tier 3: b is gone unless the higher tier lists it. It overrides the ability and adds a resource.
	five := at(5)
	src, ok = findSource(five, "feature:gift")
	if !ok || !sameIDs(sourceSpellIDs(src), "a", "c") || src.Ability != Int || src.SaveDC != 13 { // 8 + PB 3 + Int +2
		t.Errorf("level 5: %+v, want spells a and c on Intelligence with DC 13", src)
	}
	foundRes := false
	for _, r := range five.Resources {
		if r.FeatureID == "feature:gift" && r.Max == 1 && r.Recharge == RechargeLongRest {
			foundRes = true
		}
	}
	if !foundRes {
		t.Errorf("the tier's resource should apply from level 5: %+v", five.Resources)
	}
}

func TestTierSpellsWithNoBaseGrant(t *testing.T) {
	lib := tierSpellsLib(t)

	below := mustCompute(t, tierChar("later", cls("plain", 2)), lib, Ruleset{})
	if _, ok := findSource(below, "feature:later"); ok {
		t.Error("a feature whose only spells come from a tier should have no source below that tier")
	}

	src, ok := findSource(mustCompute(t, tierChar("later", cls("plain", 3)), lib, Ruleset{}), "feature:later")
	if !ok || !sameIDs(sourceSpellIDs(src), "b") || src.Ability != Wis || src.SaveDC != 13 { // 8 + PB 2 + Wis +3
		t.Errorf("level 3: %+v, want spell b on Wisdom with DC 13", src)
	}
}

// For a feature a class grants, the tier level is the level in that class, not the total level.
func TestTierSpellsUseClassLevelForClassFeatures(t *testing.T) {
	lib := tierSpellsLib(t)

	multi := mustCompute(t, Character{ID: "m", Name: "M",
		Classes: []ClassLevel{cls("caster", 2), cls("plain", 4)}}, lib, Ruleset{}) // total level 6, caster level 2
	if _, ok := findSource(multi, "feature:classgift"); ok {
		t.Error("caster level 2 is below the tier, even though the total level is 6")
	}

	single := mustCompute(t, Character{ID: "s", Name: "S", Classes: []ClassLevel{cls("caster", 3)}}, lib, Ruleset{})
	src, ok := findSource(single, "feature:classgift")
	if !ok || !sameIDs(sourceSpellIDs(src), "b") {
		t.Errorf("caster level 3 should unlock spell b: %+v", src)
	}
}

func TestLoaderValidatesTierSpells(t *testing.T) {
	bad := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"features/f.json": `[
			{"id":"ghost","name":"Ghost","origin":"race","tiers":[
			 {"min_level":3,"spells":{"spell_ids":["nope"],"ability":"cha"}}]},
			{"id":"nobody","name":"Nobody","origin":"race","tiers":[
			 {"min_level":3,"spells":{"spell_ids":[]}}]},
			{"id":"luck","name":"Luck","origin":"race","tiers":[
			 {"min_level":3,"spells":{"spell_ids":[],"ability":"luck"}}]}]`,
	}))
	for _, want := range []string{"unknown spell", "is not an ability"} {
		if !hasIssue(bad, SevError, want) {
			t.Errorf("expected an error containing %q, got %v", want, bad.Issues())
		}
	}

	// An ability may be left out of a tier when the feature's own spells name one.
	ok := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"spells/s.json": `[{"id":"a","name":"A","level":0}]`,
		"features/f.json": `{"id":"inherits","name":"Inherits","origin":"race",
			"spells":{"spell_ids":["a"],"ability":"cha"},
			"tiers":[{"min_level":3,"spells":{"spell_ids":["a"]}}]}`,
	}))
	if ok.HasErrors() {
		dumpIssues(t, ok)
		t.Error("a tier may inherit its ability from the feature's own spells")
	}
}
