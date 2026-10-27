package character

// repeatable_test.go - tests for repeatable features: a feat that can be taken more than once
// (Ability Score Improvement) where each pick keeps its own choices. Written without being able to
// run them: a failure may be a test mistake as much as a code bug.

import "testing"

func repeatableLib(t *testing.T) *ContentLibrary {
	t.Helper()
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"features/hosts.json": `[
			{"id":"host_a","name":"Host A","origin":"class","choices":[
				{"id":"feat","prompt":"Choose a feat","kind":"pick","count":1,"filter":{"type":"feat"}}]},
			{"id":"host_b","name":"Host B","origin":"class","choices":[
				{"id":"feat","prompt":"Choose a feat","kind":"pick","count":1,"filter":{"type":"feat"}}]}]`,
		"features/feats.json": `[
			{"id":"bump","name":"Bump","origin":"feat","repeatable":true,"choices":[
				{"id":"scores","prompt":"Spend two points","kind":"allocate","points":2,"max_per_option":2,"options":[
					{"id":"str","name":"Str","modifiers":[{"target":"ability.str","kind":"add","value":1}]},
					{"id":"dex","name":"Dex","modifiers":[{"target":"ability.dex","kind":"add","value":1}]}]}]},
			{"id":"once","name":"Once","origin":"feat","modifiers":[{"target":"initiative","kind":"add","value":1}]}]`,
	}))
	if lib.HasErrors() {
		dumpIssues(t, lib)
		t.Fatal("the test pack should load without errors")
	}
	return lib
}

func featPick(host, id string) Selection {
	return Selection{FeatureID: host, ChoiceID: "feat", Picked: []string{id}}
}

func repeatableChar(sel ...Selection) Character {
	c := newChar(1, nil) // every ability defaults to 10
	c.FeatureIDs = []string{"host_a", "host_b"}
	c.Selections = sel
	return c
}

func TestRepeatableFeatEachPickIsSeparate(t *testing.T) {
	lib := repeatableLib(t)
	sh := mustCompute(t, repeatableChar(
		featPick("host_a", "bump"), featPick("host_b", "bump"),
		Selection{FeatureID: "bump@host_a:feat", ChoiceID: "scores", Allocated: map[string]int{"str": 2}},
		Selection{FeatureID: "bump@host_b:feat", ChoiceID: "scores", Allocated: map[string]int{"dex": 2}},
	), lib, Ruleset{})

	if sh.Abilities[Str].Score != 12 || sh.Abilities[Dex].Score != 12 {
		t.Errorf("Str %d, Dex %d; want 12 and 12: each pick spends its own two points",
			sh.Abilities[Str].Score, sh.Abilities[Dex].Score)
	}
	if len(sh.PendingChoices) != 0 {
		t.Errorf("nothing should be pending: %+v", sh.PendingChoices)
	}
}

func TestRepeatableFeatPointsOnTheSameAbilityStack(t *testing.T) {
	lib := repeatableLib(t)
	sh := mustCompute(t, repeatableChar(
		featPick("host_a", "bump"), featPick("host_b", "bump"),
		Selection{FeatureID: "bump@host_a:feat", ChoiceID: "scores", Allocated: map[string]int{"str": 2}},
		Selection{FeatureID: "bump@host_b:feat", ChoiceID: "scores", Allocated: map[string]int{"str": 2}},
	), lib, Ruleset{})
	if sh.Abilities[Str].Score != 14 {
		t.Errorf("Str = %d, want 14: two separate picks must not dedupe each other", sh.Abilities[Str].Score)
	}
}

func TestRepeatableFeatLeavesOnePendingChoicePerPick(t *testing.T) {
	lib := repeatableLib(t)
	sh := mustCompute(t, repeatableChar(featPick("host_a", "bump"), featPick("host_b", "bump")), lib, Ruleset{})
	for _, inst := range []string{"bump@host_a:feat", "bump@host_b:feat"} {
		if p, ok := findPending(sh, inst, "scores"); !ok || p.Remaining != 2 {
			t.Errorf("expected 2 points pending under %q, got %+v", inst, sh.PendingChoices)
		}
	}
}

func TestRegularFeatTakenTwiceCountsOnce(t *testing.T) {
	lib := repeatableLib(t)
	sh := mustCompute(t, repeatableChar(featPick("host_a", "once"), featPick("host_b", "once")), lib, Ruleset{})
	if sh.Initiative != 1 { // Dex 0 + 1 once, not twice
		t.Errorf("initiative = %d, want 1: a non-repeatable feat granted twice applies once", sh.Initiative)
	}
}

func TestChoiceOptionsKnowAboutRepeatables(t *testing.T) {
	lib := repeatableLib(t)
	c := repeatableChar(featPick("host_a", "bump"))
	c.FeatureIDs = append(c.FeatureIDs, "once") // the character already has the regular feat

	opts, err := ChoiceOptions(c, lib, Ruleset{}, "host_b", "feat")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]EligibleOption{}
	for _, o := range opts {
		byID[o.OptionID] = o
	}
	if !byID["bump"].Eligible {
		t.Errorf("a repeatable feat should stay eligible after it has been taken: %+v", byID["bump"])
	}
	if byID["once"].Eligible || byID["once"].Reason != "already taken" {
		t.Errorf("a regular feat already taken should be ineligible: %+v", byID["once"])
	}

	// The feat's own choice is looked up by the instance id.
	scores, err := ChoiceOptions(c, lib, Ruleset{}, "bump@host_a:feat", "scores")
	if err != nil || len(scores) != 2 {
		t.Errorf("options for the instance's own choice = %+v (err %v), want 2", scores, err)
	}
}

func TestLoaderRejectsAtSignInFeatureIDs(t *testing.T) {
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"features/f.json": `{"id":"bad@id","name":"Bad","origin":"feat"}`,
	}))
	if !hasIssue(lib, SevError, "can't contain") {
		t.Errorf("expected an error about the @ in the id, got %v", lib.Issues())
	}
}

// ---------------------------------------------------------------------------
// The example pack: ability score improvement as a feat, taken at two levels
// ---------------------------------------------------------------------------

func TestExampleASITakenTwice(t *testing.T) {
	lib := loadExample(t)
	c := Character{
		ID: "r8", Name: "Veteran",
		Abilities: map[Ability]AbilityScore{
			Str: {Score: 10}, Dex: {Score: 16}, Con: {Score: 14}, Int: {Score: 12}, Wis: {Score: 10}, Cha: {Score: 10},
		},
		Classes: []ClassLevel{{ClassID: "rogue", SubclassID: "thief", Level: 8, RolledHP: []int{8, 5, 5, 5, 5, 5, 5, 5}}},
		Selections: []Selection{
			{FeatureID: "class:rogue:start", ChoiceID: "skills", Picked: []string{"stealth", "perception", "acrobatics", "sleight_of_hand"}},
			{FeatureID: "rogue_expertise_1", ChoiceID: "expertise", Picked: []string{"stealth", "sleight_of_hand"}},
			{FeatureID: "rogue_expertise_6", ChoiceID: "expertise", Picked: []string{"perception", "acrobatics"}},
			featPick("rogue_asi_4", "ability_score_improvement"),
			featPick("rogue_asi_8", "ability_score_improvement"),
			{FeatureID: "ability_score_improvement@rogue_asi_4:feat", ChoiceID: "scores", Allocated: map[string]int{"dex": 2}},
			{FeatureID: "ability_score_improvement@rogue_asi_8:feat", ChoiceID: "scores", Allocated: map[string]int{"dex": 1, "con": 1}},
		},
	}
	sh := mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)

	if sh.Abilities[Dex].Score != 19 || sh.Abilities[Con].Score != 15 { // 16+2+1, 14+1
		t.Errorf("Dex %d, Con %d; want 19 and 15", sh.Abilities[Dex].Score, sh.Abilities[Con].Score)
	}
	if left := realPending(sh); len(left) != 0 { // the fixture has no background; that item is expected
		t.Errorf("everything has been decided, but %+v is pending", left)
	}
}
