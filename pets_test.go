package character

// pets_test.go - tests for pets: owner modifiers aimed at them and their computed stat blocks.

import "testing"

func TestPetModifiersDontTouchTheOwner(t *testing.T) {
	lib := fakeLib{features: map[string]Feature{"invest": {ID: "invest", Name: "Invest", Modifiers: []Modifier{
		{Target: PetTarget("familiar", SpeedTarget(SpeedFly)), Kind: ModMin, Value: 40, SourceID: "invest"},
		{Target: PetTarget("familiar", TargetAC), Kind: ModAdd, Value: 1, SourceID: "invest"},
	}}}}
	c := newChar(1, nil)
	c.FeatureIDs = []string{"invest"}
	sh := mustCompute(t, c, lib, Ruleset{})
	if sh.Speeds[SpeedFly] != 0 || sh.AC != 10 {
		t.Errorf("owner fly %d, AC %d; want 0 and 10", sh.Speeds[SpeedFly], sh.AC)
	}
	if w, ok := findWarning(sh, WarnUnknownTarget); ok {
		t.Errorf("pet targets should be recognised: %+v", w)
	}
	if len(sh.petMods) != 2 {
		t.Errorf("petMods = %+v, want 2", sh.petMods)
	}
}
