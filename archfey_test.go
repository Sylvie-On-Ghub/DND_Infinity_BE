package character

// archfey_test.go - the Archfey warlock subclass: a subclass feature plus an expanded spell list.

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

func TestArchfeyExpandedSpellsAreLegalPicks(t *testing.T) {
	lib := loadWarlockLib(t)

	// A patron spell is a legal pick for an archfey warlock.
	c := archfeyFixture()
	sp := c.Spellbooks["class:warlock"]
	sp.Known = []string{"hex", "armor_of_agathys", "faerie_fire", "darkness", "misty_step"}
	c.Spellbooks["class:warlock"] = sp
	sh := mustCompute(t, c, lib, Ruleset{})
	if hasWarningValue(sh, WarnNotInPool, "faerie_fire") {
		t.Errorf("faerie_fire is an archfey expanded spell but was flagged: %+v", sh.Warnings)
	}

	// The same pick for a fiend warlock is flagged: the expansion belongs to the patron.
	f := warlockFixture()
	sp = f.Spellbooks["class:warlock"]
	sp.Known = []string{"hex", "armor_of_agathys", "faerie_fire", "darkness", "misty_step"}
	f.Spellbooks["class:warlock"] = sp
	sh = mustCompute(t, f, lib, Ruleset{})
	if !hasWarningValue(sh, WarnNotInPool, "faerie_fire") {
		t.Error("faerie_fire should be off-list for a fiend warlock")
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
