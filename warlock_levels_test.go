package character

// warlock_levels_test.go - the 2024 Warlock from level 5 to 20: the full class table, Mystic Arcanum
// (a spell pool whose picks join the feature's spells), the patron features at 6, 10 and 14, and
// invocations that need a pact (feature.<id> prerequisites).

import (
	"strings"
	"testing"
)

// warlockAt is the level 4 fixture raised (or lowered) to level, with the given patron.
func warlockAt(level int, subclass string) Character {
	c := warlockFixture()
	c.Classes[0].Level = level
	c.Classes[0].SubclassID = subclass
	hp := []int{8}
	for len(hp) < level {
		hp = append(hp, 5)
	}
	c.Classes[0].RolledHP = hp
	return c
}

func findFeature(sh Sheet, id string) (FeatureView, bool) {
	for _, f := range sh.Features {
		if baseFeatureID(f.FeatureID) == id {
			return f, true
		}
	}
	return FeatureView{}, false
}

func findResource(sh Sheet, id string) (ResourceView, bool) {
	for _, r := range sh.Resources {
		if r.FeatureID == id {
			return r, true
		}
	}
	return ResourceView{}, false
}

func TestWarlockLevelTwenty(t *testing.T) {
	lib := loadWarlockLib(t)
	sh := mustCompute(t, warlockAt(20, "fiend"), lib, Ruleset{})
	noContentWarnings(t, sh) // includes "the class table stops before this level"

	if sh.PactSlots == nil || sh.PactSlots.Max != 4 || sh.PactSlots.Level != 5 {
		t.Errorf("pact slots = %+v, want 4 slots of level 5", sh.PactSlots)
	}
	src, _ := findSource(sh, "class:warlock")
	if src.CantripsAllowed != 4 || src.SpellsAllowed != 15 {
		t.Errorf("cantrips %d, spells %d; want 4 and 15", src.CantripsAllowed, src.SpellsAllowed)
	}
	for _, id := range []string{"geas", "insect_plague", "fireball"} {
		if sp, ok := findSpell(src.Spells, id); !ok || !sp.Granted {
			t.Errorf("a level 20 fiend should have %s as a patron spell: %+v (found %v)", id, sp, ok)
		}
	}

	// Every choice the table adds is waiting to be made.
	want := []struct {
		feature, choice string
		remaining       int
	}{
		{"eldritch_invocations_5", "invocations", 2},
		{"eldritch_invocations_7", "invocations", 1},
		{"eldritch_invocations_9", "invocations", 1},
		{"eldritch_invocations_12", "invocations", 1},
		{"eldritch_invocations_15", "invocations", 1},
		{"eldritch_invocations_18", "invocations", 1},
		{"warlock_asi_4", "feat", 1},
		{"warlock_asi_8", "feat", 1},
		{"warlock_asi_12", "feat", 1},
		{"warlock_asi_16", "feat", 1},
		{"warlock_epic_boon", "feat", 1},
		{"mystic_arcanum_6", "spell", 1},
		{"mystic_arcanum_7", "spell", 1},
		{"mystic_arcanum_8", "spell", 1},
		{"mystic_arcanum_9", "spell", 1},
	}
	for _, w := range want {
		p, ok := findPending(sh, w.feature, w.choice)
		if !ok || p.Remaining != w.remaining {
			t.Errorf("pending %s/%s = %+v (found %v), want %d remaining", w.feature, w.choice, p, ok, w.remaining)
		}
	}

	// Ten invocations in all, as the 2024 table has at level 18 and up.
	total := 0
	for _, f := range sh.Features {
		if !strings.HasPrefix(f.FeatureID, "eldritch_invocations_") {
			continue
		}
		feat, err := lib.Feature(f.FeatureID)
		if err != nil {
			t.Fatal(err)
		}
		total += feat.Choices[0].Count
	}
	if total != 10 {
		t.Errorf("invocations at level 20 = %d, want 10", total)
	}
}

func TestWarlockFeaturesArriveAtTheirLevels(t *testing.T) {
	lib := loadWarlockLib(t)
	at := map[string]struct {
		level    int
		subclass string
	}{
		"magical_cunning":       {2, "fiend"},
		"contact_patron":        {9, "fiend"},
		"mystic_arcanum_6":      {11, "fiend"},
		"mystic_arcanum_7":      {13, "fiend"},
		"mystic_arcanum_8":      {15, "fiend"},
		"mystic_arcanum_9":      {17, "fiend"},
		"warlock_epic_boon":     {19, "fiend"},
		"eldritch_master":       {20, "fiend"},
		"dark_ones_own_luck":    {6, "fiend"},
		"fiendish_resilience":   {10, "fiend"},
		"hurl_through_hell":     {14, "fiend"},
		"misty_escape":          {6, "archfey"},
		"beguiling_defenses":    {10, "archfey"},
		"bewitching_magic":      {14, "archfey"},
		"psychic_spells":        {3, "great_old_one"},
		"clairvoyant_combatant": {6, "great_old_one"},
		"eldritch_hex":          {10, "great_old_one"},
		"thought_shield":        {10, "great_old_one"},
		"create_thrall":         {14, "great_old_one"},
	}
	for id, w := range at {
		if _, ok := findFeature(mustCompute(t, warlockAt(w.level-1, w.subclass), lib, Ruleset{}), id); ok {
			t.Errorf("%s should not be there at level %d", id, w.level-1)
		}
		f, ok := findFeature(mustCompute(t, warlockAt(w.level, w.subclass), lib, Ruleset{}), id)
		if !ok || f.Level != w.level {
			t.Errorf("%s should arrive at level %d: %+v (found %v)", id, w.level, f, ok)
		}
	}
}

func TestMagicalCunningAndContactPatronAreOncePerLongRest(t *testing.T) {
	lib := loadWarlockLib(t)
	sh := mustCompute(t, warlockAt(9, "fiend"), lib, Ruleset{})
	if r, ok := findResource(sh, "magical_cunning"); !ok || r.Max != 1 || r.Recharge != RechargeLongRest {
		t.Errorf("magical cunning resource = %+v (found %v), want 1 per long rest", r, ok)
	}
	src, ok := findSource(sh, "feature:contact_patron")
	if !ok {
		t.Fatalf("Contact Patron should add a spell source: %+v", sh.Spellcasting)
	}
	if _, ok := findSpell(src.Spells, "contact_other_plane"); !ok {
		t.Errorf("Contact Patron should cast Contact Other Plane: %+v", src.Spells)
	}
	if r, ok := findResource(sh, "feature:contact_patron"); !ok || r.Max != 1 || r.Recharge != RechargeLongRest {
		t.Errorf("contact patron resource = %+v (found %v), want 1 per long rest", r, ok)
	}
}

func TestMysticArcanum(t *testing.T) {
	lib := loadWarlockLib(t)
	c := warlockAt(11, "fiend")

	// Nothing picked yet: the choice waits, and there's nothing to cast or track.
	sh := mustCompute(t, c, lib, Ruleset{})
	if _, ok := findPending(sh, "mystic_arcanum_6", "spell"); !ok {
		t.Error("the level 6 arcanum should be pending")
	}
	if _, ok := findSource(sh, "feature:mystic_arcanum_6"); ok {
		t.Error("an arcanum with no spell picked should not add a spell source")
	}
	if _, ok := findResource(sh, "feature:mystic_arcanum_6"); ok {
		t.Error("an arcanum with no spell picked should not add a resource")
	}

	// The pool is the level 6 spells on the warlock list.
	opts, err := ChoiceOptions(c, lib, Ruleset{}, "mystic_arcanum_6", "spell")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, o := range opts {
		names = append(names, o.Name)
		if !o.Eligible {
			t.Errorf("%s should be eligible: %+v", o.OptionID, o)
		}
	}
	if got, want := strings.Join(names, ", "), "Arcane Gate, Circle of Death, Create Undead, Eyebite, Find the Path, Mass Suggestion, Temi's Bubbling Cauldron, True Seeing"; got != want {
		t.Errorf("arcanum options = %s\nwant %s", got, want)
	}

	// Picked: castable once per long rest at the warlock's DC (8 + PB 4 + Cha 3).
	c.Selections = append(c.Selections, Selection{FeatureID: "mystic_arcanum_6", ChoiceID: "spell", Picked: []string{"circle_of_death"}})
	sh = mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	if _, ok := findPending(sh, "mystic_arcanum_6", "spell"); ok {
		t.Error("the arcanum should no longer be pending")
	}
	src, ok := findSource(sh, "feature:mystic_arcanum_6")
	if !ok {
		t.Fatalf("the arcanum should add a spell source: %+v", sh.Spellcasting)
	}
	if sp, ok := findSpell(src.Spells, "circle_of_death"); !ok || sp.SaveDC != 15 {
		t.Errorf("circle of death = %+v (found %v), want DC 15", sp, ok)
	}
	r, ok := findResource(sh, "feature:mystic_arcanum_6")
	if !ok || r.Max != 1 || r.Recharge != RechargeLongRest {
		t.Errorf("arcanum resource = %+v (found %v), want 1 per long rest", r, ok)
	}
	if f, _ := findFeature(sh, "mystic_arcanum_6"); f.ResourceID != r.FeatureID {
		t.Errorf("the arcanum feature's resource_id = %q, want %q", f.ResourceID, r.FeatureID)
	}
	if hasWarning(sh, WarnNotInPool) {
		t.Errorf("an on-list pick should not warn: %+v", sh.Warnings)
	}
}

func TestMysticArcanumOffListPickWarnsButApplies(t *testing.T) {
	lib := loadWarlockLib(t)
	c := warlockAt(11, "fiend")
	c.Selections = append(c.Selections, Selection{FeatureID: "mystic_arcanum_6", ChoiceID: "spell", Picked: []string{"fireball"}})
	sh := mustCompute(t, c, lib, Ruleset{})
	if !hasWarningValue(sh, WarnNotInPool, "fireball") {
		t.Errorf("a level 3 spell off the warlock list should warn: %+v", sh.Warnings)
	}
	src, _ := findSource(sh, "feature:mystic_arcanum_6")
	if _, ok := findSpell(src.Spells, "fireball"); !ok {
		t.Errorf("the pick should still apply: %+v", src.Spells)
	}
}

func TestInvocationsThatNeedAPact(t *testing.T) {
	lib := loadWarlockLib(t)
	c := warlockAt(5, "fiend")
	c.Selections = append(c.Selections, Selection{FeatureID: "eldritch_invocations_5", ChoiceID: "invocations", Picked: []string{"thirsting_blade", "eldritch_mind"}})

	sh := mustCompute(t, c, lib, Ruleset{})
	if !hasWarningValue(sh, WarnPrereqUnmet, "feature.pact_of_the_blade>=1") {
		t.Errorf("Thirsting Blade without Pact of the Blade should warn: %+v", sh.Warnings)
	}
	thirsting := func(c Character) EligibleOption {
		opts, err := ChoiceOptions(c, lib, Ruleset{}, "eldritch_invocations_5", "invocations")
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range opts {
			if o.OptionID == "thirsting_blade" {
				return o
			}
		}
		t.Fatal("thirsting_blade is not offered")
		return EligibleOption{}
	}
	if o := thirsting(c); o.Eligible || o.Reason != "Pact of the Blade invocation" {
		t.Errorf("thirsting blade = %+v, want ineligible for want of the pact", o)
	}

	// Take the pact at level 1 instead of Eldritch Sight, and both are fine.
	for i, s := range c.Selections {
		if s.FeatureID == "eldritch_invocations_1" {
			c.Selections[i].Picked = []string{"pact_of_the_blade"}
		}
	}
	sh = mustCompute(t, c, lib, Ruleset{})
	if hasWarning(sh, WarnPrereqUnmet) {
		t.Errorf("with Pact of the Blade no prerequisite should be unmet: %+v", sh.Warnings)
	}
	if o := thirsting(c); !o.Eligible {
		t.Errorf("thirsting blade = %+v, want eligible with the pact", o)
	}
}

func TestPatronFeaturesAtFourteen(t *testing.T) {
	lib := loadWarlockLib(t)

	fiend := mustCompute(t, warlockAt(14, "fiend"), lib, Ruleset{})
	if r, ok := findResource(fiend, "dark_ones_own_luck"); !ok || r.Max != 3 || r.Recharge != RechargeLongRest {
		t.Errorf("Dark One's Own Luck = %+v (found %v), want Cha mod (3) per long rest", r, ok)
	}
	if r, ok := findResource(fiend, "hurl_through_hell"); !ok || r.Max != 1 {
		t.Errorf("Hurl Through Hell = %+v (found %v), want 1 use", r, ok)
	}

	goo := mustCompute(t, warlockAt(14, "great_old_one"), lib, Ruleset{})
	if !containsDamage(goo.Resistances, "psychic") {
		t.Errorf("Thought Shield should resist psychic: %v", goo.Resistances)
	}
	if r, ok := findResource(goo, "clairvoyant_combatant"); !ok || r.Recharge != RechargeShortRest {
		t.Errorf("Clairvoyant Combatant = %+v (found %v), want a short rest recharge", r, ok)
	}
	src, _ := findSource(goo, "class:warlock")
	for _, id := range []string{"hex", "dissonant_whispers", "telekinesis"} {
		if sp, ok := findSpell(src.Spells, id); !ok || !sp.Granted {
			t.Errorf("a level 14 Great Old One warlock should have %s granted: %+v (found %v)", id, sp, ok)
		}
	}

	archfey := mustCompute(t, warlockAt(14, "archfey"), lib, Ruleset{})
	if !containsDamage(archfey.Immunities, "charmed") {
		t.Errorf("Beguiling Defenses should make the warlock immune to Charmed: %v", archfey.Immunities)
	}
	sawEscape := false
	for _, a := range archfey.Actions {
		if a.Name == "Misty Escape" && a.Type == ActionReaction {
			sawEscape = true
		}
	}
	if !sawEscape {
		t.Errorf("Misty Escape should be a reaction: %+v", archfey.Actions)
	}
}

func containsDamage(list []DamageType, want DamageType) bool {
	for _, d := range list {
		if d == want {
			return true
		}
	}
	return false
}

func TestLoaderChecksSpellPools(t *testing.T) {
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"spells/s.json":      `[{"id":"gate","name":"Gate","level":9}]`,
		"spell_lists/l.json": `[{"id":"warlock","name":"Warlock","spell_ids":["gate"]}]`,
		"features/f.json": `[
			{"id":"no_block","name":"No Block","choices":[{"id":"spell","prompt":"Pick","kind":"pick","count":1,
				"filter":{"type":"spell","spell_list":"warlock","level":9}}]},
			{"id":"bad_list","name":"Bad List","spells":{"spell_ids":[],"ability":"cha"},"choices":[{"id":"spell","prompt":"Pick","kind":"pick","count":1,
				"filter":{"type":"spell","spell_list":"no_such_list"}}]},
			{"id":"bad_prereq","name":"Bad Prereq","prerequisites":[{"description":"Something","target":"feature.no_such_feature","min":1}]}]`,
	}))
	if !hasIssue(lib, SevError, "needs a spells block") {
		t.Error("a spell pool on a feature with no spells block should be an error")
	}
	if !hasIssue(lib, SevError, `unknown spell_list "no_such_list"`) {
		t.Error("a spell pool naming an unknown list should be an error")
	}
	if !hasIssue(lib, SevWarning, `target "feature.no_such_feature" can't be checked`) {
		t.Error("a feature prerequisite naming an unknown feature should warn")
	}
}
