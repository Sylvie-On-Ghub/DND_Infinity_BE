package character

// features_test.go - tests for Sheet.Features (every feature the character has, with where it came
// from) and for actions carrying the id of the feature that granted them.

import (
	"strings"
	"testing"
)

func featuresLib(t *testing.T) *ContentLibrary {
	t.Helper()
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"classes/fighter.json": `[{"id":"fighter","name":"Fighter","hit_die":10,
			"levels":[["second_wind"],["action_surge"],[],["fighter_asi_4"]],
			"subclass_level":3,
			"subclasses":[{"id":"champion","name":"Champion","levels":[[],[],["improved_critical"]]}]}]`,
		"features/fighter.json": `[
			{"id":"second_wind","name":"Second Wind","origin":"class","source_id":"fighter","description":"Heal yourself.",
				"actions":[{"name":"Second Wind","type":"bonus_action"}],
				"resource":{"name":"Second Wind","max":1,"recharge":"short_rest"}},
			{"id":"action_surge","name":"Action Surge","origin":"class","source_id":"fighter"},
			{"id":"improved_critical","name":"Improved Critical","origin":"class","source_id":"fighter"},
			{"id":"fighter_asi_4","name":"Ability Score Improvement","origin":"class","source_id":"fighter","choices":[
				{"id":"feat","prompt":"Choose a feat","kind":"pick","count":1,"filter":{"type":"feat"}}]}]`,
		"features/race.json": `[
			{"id":"elf","name":"Elf","origin":"race","tags":["race"],"grants":{"feature_ids":["darkvision"]}},
			{"id":"darkvision","name":"Darkvision","origin":"race","description":"See in the dark.",
				"modifiers":[{"target":"sense.darkvision","kind":"add","value":60}]}]`,
		"features/feats.json": `[
			{"id":"lucky","name":"Lucky","origin":"feat","actions":[{"name":"Lucky reroll","type":"free"}]},
			{"id":"mystery","name":"Mystery"}]`,
	}))
	if lib.HasErrors() {
		dumpIssues(t, lib)
		t.Fatal("the test pack should load without errors")
	}
	return lib
}

func featuresChar() Character {
	c := newChar(1, nil)
	c.Classes = []ClassLevel{{ClassID: "fighter", Level: 4, SubclassID: "champion", RolledHP: []int{10, 6, 6, 6}}}
	c.RaceID = "elf"
	c.FeatureIDs = []string{"mystery"}
	c.Selections = []Selection{{FeatureID: "fighter_asi_4", ChoiceID: "feat", Picked: []string{"lucky"}}}
	return c
}

func TestSheetFeaturesListsEverySourceWithItsOrigin(t *testing.T) {
	sh := mustCompute(t, featuresChar(), featuresLib(t), Ruleset{})

	got := map[string]FeatureView{}
	for _, f := range sh.Features {
		if _, dup := got[f.FeatureID]; dup {
			t.Errorf("%s is listed twice", f.FeatureID)
		}
		got[f.FeatureID] = f
	}
	want := []FeatureView{
		{FeatureID: "second_wind", Name: "Second Wind", Description: "Heal yourself.", Source: OriginClass, SourceID: "fighter", Level: 1, ResourceID: "second_wind"},
		{FeatureID: "action_surge", Name: "Action Surge", Source: OriginClass, SourceID: "fighter", Level: 2},
		{FeatureID: "improved_critical", Name: "Improved Critical", Source: OriginClass, SourceID: "fighter", Level: 3},
		{FeatureID: "fighter_asi_4", Name: "Ability Score Improvement", Source: OriginClass, SourceID: "fighter", Level: 4},
		// A feat is its own source; it records the class level and the feature it was picked from.
		{FeatureID: "lucky", Name: "Lucky", Source: OriginFeat, SourceID: "lucky", GrantedBy: "fighter_asi_4", Level: 4},
		// The race itself, and a trait it grants that names no source of its own.
		{FeatureID: "elf", Name: "Elf", Source: OriginRace, SourceID: "elf"},
		{FeatureID: "darkvision", Name: "Darkvision", Description: "See in the dark.", Source: OriginRace, SourceID: "elf", GrantedBy: "elf"},
		// No origin and nothing to infer it from.
		{FeatureID: "mystery", Name: "Mystery", Source: OriginOther},
	}
	for _, w := range want {
		if g, ok := got[w.FeatureID]; !ok {
			t.Errorf("%s missing from features: %+v", w.FeatureID, sh.Features)
		} else if g != w {
			t.Errorf("%s:\n got %+v\nwant %+v", w.FeatureID, g, w)
		}
	}
	if len(sh.Features) != len(want) {
		t.Errorf("%d features, want %d: %+v", len(sh.Features), len(want), sh.Features)
	}
	for _, f := range sh.Features {
		if strings.HasPrefix(f.FeatureID, "class:") {
			t.Errorf("internal feature %s should not be listed", f.FeatureID)
		}
	}
}

func TestSheetFeaturesIsEmptyNotNull(t *testing.T) {
	sh := mustCompute(t, newChar(1, nil), featuresLib(t), Ruleset{})
	if sh.Features == nil {
		t.Error("features should be an empty list, not null")
	}
}

func TestActionsCarryTheirFeatureID(t *testing.T) {
	sh := mustCompute(t, featuresChar(), featuresLib(t), Ruleset{})

	byName := map[string]Action{}
	for _, a := range sh.Actions {
		byName[a.Name] = a
	}
	if a := byName["Second Wind"]; a.FeatureID != "second_wind" {
		t.Errorf("Second Wind action feature_id = %q, want second_wind", a.FeatureID)
	}
	if a := byName["Lucky reroll"]; a.FeatureID != "lucky" || a.ResourceID != "" {
		t.Errorf("Lucky action feature_id = %q, resource_id = %q; want lucky and none", a.FeatureID, a.ResourceID)
	}

	// resource_id joins to the resource, which is the point of having it.
	if !hasResource(sh, byName["Second Wind"].ResourceID) {
		t.Errorf("no resource matches the Second Wind action's resource_id %q: %+v", byName["Second Wind"].ResourceID, sh.Resources)
	}
	for _, f := range sh.Features {
		if f.FeatureID == "second_wind" && f.ResourceID != "second_wind" {
			t.Errorf("Second Wind feature resource_id = %q, want second_wind", f.ResourceID)
		}
	}
}

func hasResource(sh Sheet, id string) bool {
	for _, r := range sh.Resources {
		if id != "" && r.FeatureID == id {
			return true
		}
	}
	return false
}

// A resource attached to a feature's spells is keyed "feature:<id>"; resource_id points at it.
func TestActionResourceIDFollowsASpellGrantResource(t *testing.T) {
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"spells/s.json": `[{"id":"misty_step","name":"Misty Step","level":2}]`,
		"features/f.json": `[{"id":"fey_step","name":"Fey Step","origin":"race",
			"actions":[{"name":"Fey Step","type":"bonus_action"}],
			"spells":{"spell_ids":["misty_step"],"ability":"cha","resource":{"name":"Fey Step","max":1,"recharge":"long_rest"}}}]`,
	}))
	if lib.HasErrors() {
		dumpIssues(t, lib)
		t.Fatal("the test pack should load without errors")
	}
	c := newChar(1, nil)
	c.FeatureIDs = []string{"fey_step"}
	sh := mustCompute(t, c, lib, Ruleset{})

	if len(sh.Actions) != 1 || sh.Actions[0].FeatureID != "fey_step" || sh.Actions[0].ResourceID != "feature:fey_step" {
		t.Fatalf("want one action with feature_id fey_step and resource_id feature:fey_step: %+v", sh.Actions)
	}
	if !hasResource(sh, sh.Actions[0].ResourceID) {
		t.Errorf("resource_id %q matches no resource: %+v", sh.Actions[0].ResourceID, sh.Resources)
	}
}

func TestRepeatableFeatInstancesAreSeparateFeatures(t *testing.T) {
	sh := mustCompute(t, repeatableChar(featPick("host_a", "bump"), featPick("host_b", "bump")), repeatableLib(t), Ruleset{})
	n := 0
	for _, f := range sh.Features {
		if baseFeatureID(f.FeatureID) == "bump" {
			n++
			if f.SourceID != "bump" {
				t.Errorf("%s source_id = %q, want bump", f.FeatureID, f.SourceID)
			}
		}
	}
	if n != 2 {
		t.Errorf("want 2 instances of bump, got %d: %+v", n, sh.Features)
	}
}
