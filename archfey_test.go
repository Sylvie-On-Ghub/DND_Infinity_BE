package character

// archfey_test.go - the Archfey warlock subclass: a subclass feature plus patron spells it grants.

import "testing"

func archfeyFixture() Character {
	c := warlockFixture()
	c.Classes[0].SubclassID = "archfey"
	return c
}

func TestArchfeyChosenClearsSubclassPending(t *testing.T) {
	lib := loadWarlockLib(t)
	sh := mustCompute(t, archfeyFixture(), lib, Ruleset{})
	if _, ok := findPending(sh, "class:warlock", "subclass"); ok {
		t.Error("subclass should not be pending once archfey is chosen")
	}
}

func TestArchfeyStepsOfTheFey(t *testing.T) {
	lib := loadWarlockLib(t)
	steps := func(c Character) (ResourceView, bool) {
		sh := mustCompute(t, c, lib, Ruleset{})
		for _, r := range sh.Resources {
			if r.FeatureID == "feature:steps_of_the_fey" {
				return r, true
			}
		}
		return ResourceView{}, false
	}

	// Uses equal the Charisma modifier: 16 Cha gives three, back on a long rest.
	r, ok := steps(archfeyFixture())
	if !ok {
		t.Fatal("Steps of the Fey resource missing at warlock level 4")
	}
	if r.Max != 3 || r.Recharge != RechargeLongRest {
		t.Errorf("steps of the fey resource = %+v, want 3 uses per long rest", r)
	}

	// A low Charisma still gets one use.
	c := archfeyFixture()
	c.Abilities[Cha] = AbilityScore{Score: 8}
	if r, _ := steps(c); r.Max != 1 {
		t.Errorf("Cha 8 gives %d uses, want the minimum of 1", r.Max)
	}

	// Not before level 3.
	c = archfeyFixture()
	c.Classes[0].Level = 2
	c.Classes[0].RolledHP = c.Classes[0].RolledHP[:2]
	if _, ok := steps(c); ok {
		t.Error("Steps of the Fey should not appear at warlock level 2")
	}
}

func TestArchfeyGrantsPatronSpells(t *testing.T) {
	lib := loadWarlockLib(t)

	// The fixture knows hex, armor_of_agathys, hellish_rebuke, darkness and misty_step: 5 of 5 at level 4.
	sh := mustCompute(t, archfeyFixture(), lib, Ruleset{})
	src, ok := findSource(sh, "class:warlock")
	if !ok {
		t.Fatal("no warlock spell source")
	}
	if src.SpellsAllowed != 5 {
		t.Errorf("spells allowed = %d, want 5: patron spells don't change it", src.SpellsAllowed)
	}
	granted := map[string]bool{}
	count := map[string]int{}
	for _, sp := range src.Spells {
		count[sp.ID]++
		if sp.Granted {
			granted[sp.ID] = true
		}
	}
	for _, id := range []string{"calm_emotions", "faerie_fire", "misty_step", "phantasmal_force", "sleep"} {
		if !granted[id] {
			t.Errorf("%s should be listed as a granted warlock spell: %+v", id, src.Spells)
		}
	}
	if count["misty_step"] != 1 {
		t.Errorf("misty_step is both granted and picked; it should be listed once, not %d times", count["misty_step"])
	}
	if granted["hex"] {
		t.Error("hex was picked, not granted")
	}

	// misty_step is granted, so picking it too doesn't use up a pick: one pick is still open.
	if p, ok := findPending(sh, "spells:class:warlock", "known"); !ok || p.Remaining != 1 {
		t.Errorf("want 1 spell known still to choose, got %+v (ok=%v)", p, ok)
	}
	if hasWarningValue(sh, WarnOverCount, "6/5") || hasWarningValue(sh, WarnNotInPool, "faerie_fire") {
		t.Errorf("patron spells should neither count nor be off-list: %+v", sh.Warnings)
	}
}

func TestArchfeyPatronSpellsNeedLevelThreeAndThePatron(t *testing.T) {
	lib := loadWarlockLib(t)
	hasGranted := func(c Character) bool {
		sh := mustCompute(t, c, lib, Ruleset{})
		src, _ := findSource(sh, "class:warlock")
		for _, sp := range src.Spells {
			if sp.Granted {
				return true
			}
		}
		return false
	}

	c := archfeyFixture()
	c.Classes[0].Level = 2
	c.Classes[0].RolledHP = c.Classes[0].RolledHP[:2]
	if hasGranted(c) {
		t.Error("archfey patron spells start at warlock level 3")
	}
	fiend := mustCompute(t, warlockFixture(), lib, Ruleset{})
	fsrc, _ := findSource(fiend, "class:warlock")
	for _, sp := range fsrc.Spells {
		if sp.Granted && (sp.ID == "faerie_fire" || sp.ID == "calm_emotions" || sp.ID == "sleep") {
			t.Errorf("a fiend warlock gets fiend spells, not the archfey's %s", sp.ID)
		}
	}

	// Picking a patron spell as a fiend is off-list: the spells belong to the patron.
	f := warlockFixture()
	sp := f.Spellbooks["class:warlock"]
	sp.Known = []string{"hex", "armor_of_agathys", "faerie_fire", "darkness", "misty_step"}
	f.Spellbooks["class:warlock"] = sp
	if sh := mustCompute(t, f, lib, Ruleset{}); !hasWarningValue(sh, WarnNotInPool, "faerie_fire") {
		t.Error("faerie_fire should be off-list for a fiend warlock")
	}
}

func TestLoaderRejectsUnknownGrantedSpell(t *testing.T) {
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"classes/c.json": `[{"id":"c","name":"C","hit_die":8,"levels":[[]],"subclasses":[
			{"id":"s","name":"S","levels":[[]],"granted_spells":[{"min_level":1,"spell_ids":["no_such_spell"]}]}]}]`,
	}))
	if !lib.HasErrors() {
		t.Error("an unknown granted spell should be a content error")
	}
}

func TestLoaderRejectsUnknownExpandedSpell(t *testing.T) {
	lib := loadWarlockLib(t)
	cls, err := lib.Class("warlock")
	if err != nil {
		t.Fatal(err)
	}
	errorCount := func() int {
		n := 0
		for _, is := range lib.Issues() {
			if is.Severity == SevError {
				n++
			}
		}
		return n
	}
	v := &validator{l: lib, skills: map[string]bool{}}
	v.class(cls)
	before := errorCount()

	cls.Subclasses = append(cls.Subclasses, Subclass{ID: "bad", Name: "Bad", ExpandedSpells: []string{"no_such_spell"}})
	v.class(cls)
	if errorCount() <= before {
		t.Error("an unknown expanded spell should be a content error")
	}
}
