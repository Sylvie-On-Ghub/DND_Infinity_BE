package character

// petactions_test.go - tests for adding, editing and tracking pets in play.

import (
	"errors"
	"testing"
)

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var re *reqError
	if !errors.As(err, &re) || re.Code != code {
		t.Errorf("err = %v, want a %s request error", err, code)
	}
}

// act runs a pet action against a freshly computed sheet, as the API does.
func act(t *testing.T, c *Character, lib Library, fn func(sh Sheet) error) error {
	t.Helper()
	return fn(mustCompute(t, *c, lib, Ruleset{}))
}

func TestAddPet(t *testing.T) {
	lib := petLib()
	c := caster()
	var id string
	err := act(t, &c, lib, func(sh Sheet) (err error) {
		id, err = AddPet(&c, sh, lib, AddPetRequest{CreatureID: "beast_land", SourceID: "companion_feat", Name: "Rex"})
		return err
	})
	if err != nil || id == "" || len(c.Pets) != 1 {
		t.Fatalf("AddPet: id %q, err %v, pets %+v", id, err, c.Pets)
	}
	if p := c.Pets[0]; p.ID != id || p.Name != "Rex" || p.Status != PetActive || p.CurrentHP != 20 {
		t.Errorf("pet = %+v, want Rex, active, 20 HP", p)
	}

	for _, req := range []AddPetRequest{{SourceID: "find_familiar"}, {CreatureID: "owl"}, {CreatureID: "ghost", SourceID: "find_familiar"}} {
		wantCode(t, act(t, &c, lib, func(sh Sheet) error { _, err := AddPet(&c, sh, lib, req); return err }), "bad_request")
	}
	if len(c.Pets) != 1 {
		t.Errorf("failed adds should add nothing: %+v", c.Pets)
	}
}

func beastChar() Character {
	return caster(Pet{ID: "b", Name: "Rex", CreatureID: "beast_land", SourceID: "companion_feat", Status: PetActive,
		CurrentHP: 12, TempHP: 3, Conditions: []Condition{"prone"}})
}

func strp(s string) *string { return &s }

func TestPatchPetFormSwap(t *testing.T) {
	lib := petLib()
	c := beastChar()
	if err := act(t, &c, lib, func(sh Sheet) error { return PatchPet(&c, sh, lib, "b", PetPatch{CreatureID: strp("owl")}) }); err != nil {
		t.Fatal(err)
	}
	if p := c.Pets[0]; p.ID != "b" || p.Name != "Rex" || p.CurrentHP != 1 || p.TempHP != 0 || len(p.Conditions) != 0 {
		t.Errorf("after the swap: %+v, want same id and name, 1 HP, no temp, no conditions", p)
	}

	before := c.Pets[0]
	wantCode(t, act(t, &c, lib, func(sh Sheet) error { return PatchPet(&c, sh, lib, "b", PetPatch{CreatureID: strp("ghost")}) }), "bad_request")
	asleep := PetStatus("asleep")
	wantCode(t, act(t, &c, lib, func(sh Sheet) error { return PatchPet(&c, sh, lib, "b", PetPatch{Status: &asleep}) }), "bad_request")
	if c.Pets[0].CreatureID != before.CreatureID || c.Pets[0].Status != before.Status {
		t.Errorf("failed patches should change nothing: %+v", c.Pets[0])
	}

	mods := []Modifier{{Target: TargetAC, Kind: ModAdd, Value: 1}}
	if err := act(t, &c, lib, func(sh Sheet) error {
		return PatchPet(&c, sh, lib, "b", PetPatch{Name: strp("Hoot"), Notes: strp("loud"), Modifiers: &mods})
	}); err != nil || c.Pets[0].Name != "Hoot" || c.Pets[0].Notes != "loud" || len(c.Pets[0].Modifiers) != 1 {
		t.Errorf("patch fields: %+v, %v", c.Pets[0], err)
	}
}

func TestPetHP(t *testing.T) {
	lib := petLib()
	c := beastChar()
	c.Pets[0].CurrentHP, c.Pets[0].TempHP = 20, 5
	hp := func(a HPAction) {
		t.Helper()
		if err := act(t, &c, lib, func(sh Sheet) error { return ApplyPetHP(&c, sh, "b", a) }); err != nil {
			t.Fatal(err)
		}
	}
	want := func(cur, temp int, s PetStatus) {
		t.Helper()
		if p := c.Pets[0]; p.CurrentHP != cur || p.TempHP != temp || p.Status != s {
			t.Errorf("pet HP %d temp %d %s; want %d %d %s", p.CurrentHP, p.TempHP, p.Status, cur, temp, s)
		}
	}
	hp(HPAction{Damage: intp(8)})
	want(17, 0, PetActive)
	hp(HPAction{Heal: intp(100)})
	want(20, 0, PetActive)
	hp(HPAction{Damage: intp(30)})
	want(0, 0, PetDead)
	hp(HPAction{Heal: intp(5)})
	want(0, 0, PetDead)

	active := PetActive
	if err := act(t, &c, lib, func(sh Sheet) error { return PatchPet(&c, sh, lib, "b", PetPatch{Status: &active}) }); err != nil {
		t.Fatal(err)
	}
	want(20, 0, PetActive)

	wantCode(t, act(t, &c, lib, func(sh Sheet) error { return ApplyPetHP(&c, sh, "b", HPAction{}) }), "bad_request")
}

func TestPetConditions(t *testing.T) {
	c := beastChar()
	c.Pets[0].Conditions = nil
	for i := 0; i < 2; i++ {
		if err := ApplyPetConditions(&c, "b", ConditionsAction{Add: []Condition{"prone"}}); err != nil {
			t.Fatal(err)
		}
	}
	if len(c.Pets[0].Conditions) != 1 {
		t.Errorf("conditions = %v, want just prone", c.Pets[0].Conditions)
	}
	if err := ApplyPetConditions(&c, "b", ConditionsAction{Remove: []Condition{"prone"}}); err != nil || len(c.Pets[0].Conditions) != 0 {
		t.Errorf("after remove: %v, %v", c.Pets[0].Conditions, err)
	}
	wantCode(t, ApplyPetConditions(&c, "b", ConditionsAction{Exhaustion: intp(1)}), "bad_request")
	if c.Exhaustion != 0 {
		t.Error("a pet's conditions must not touch the owner")
	}
}

func TestRemovePetAndUnknownPets(t *testing.T) {
	lib := petLib()
	c := beastChar()
	wantCode(t, RemovePet(&c, "nope"), "not_found")
	wantCode(t, ApplyPetConditions(&c, "nope", ConditionsAction{}), "not_found")
	wantCode(t, act(t, &c, lib, func(sh Sheet) error { return ApplyPetHP(&c, sh, "nope", HPAction{Heal: intp(1)}) }), "not_found")
	wantCode(t, act(t, &c, lib, func(sh Sheet) error { return PatchPet(&c, sh, lib, "nope", PetPatch{}) }), "not_found")
	if err := RemovePet(&c, "b"); err != nil || len(c.Pets) != 0 {
		t.Errorf("remove: %v, pets %+v", err, c.Pets)
	}
}

func TestLongRestHealsPets(t *testing.T) {
	lib := petLib()
	c := caster(
		Pet{ID: "a", CreatureID: "beast_land", SourceID: "companion_feat", Status: PetActive, CurrentHP: 3, TempHP: 2},
		Pet{ID: "d", CreatureID: "owl", SourceID: "find_familiar", Status: PetDismissed, CurrentHP: 0, TempHP: 1},
		Pet{ID: "x", CreatureID: "owl", SourceID: "find_familiar", Status: PetDead, CurrentHP: 0})
	if err := act(t, &c, lib, func(sh Sheet) error { return ApplyRest(&c, sh, "short") }); err != nil {
		t.Fatal(err)
	}
	if c.Pets[0].CurrentHP != 3 || c.Pets[1].TempHP != 1 {
		t.Errorf("a short rest should leave pets alone: %+v", c.Pets)
	}
	if err := act(t, &c, lib, func(sh Sheet) error { return ApplyRest(&c, sh, "long") }); err != nil {
		t.Fatal(err)
	}
	if p := c.Pets[0]; p.CurrentHP != 20 || p.TempHP != 0 {
		t.Errorf("active pet after a long rest: %+v", p)
	}
	if p := c.Pets[1]; p.CurrentHP != 1 || p.TempHP != 0 {
		t.Errorf("dismissed pet after a long rest: %+v", p)
	}
	if p := c.Pets[2]; p.CurrentHP != 0 || p.Status != PetDead {
		t.Errorf("a dead pet stays dead: %+v", p)
	}
}

func TestLongRestKeepsAnOrphanedPetsHP(t *testing.T) {
	lib := petLib()
	c := caster(Pet{ID: "g", CreatureID: "gone", SourceID: "find_familiar", Status: PetActive, CurrentHP: 4, TempHP: 2})
	if err := act(t, &c, lib, func(sh Sheet) error { return ApplyRest(&c, sh, "long") }); err != nil {
		t.Fatal(err)
	}
	if p := c.Pets[0]; p.CurrentHP != 4 || p.TempHP != 2 {
		t.Errorf("a pet whose creature is missing has no known max HP, so a rest must leave it alone: %+v", p)
	}
}

func TestRecastingADeadPetBringsItBack(t *testing.T) {
	lib := petLib()
	c := caster(Pet{ID: "o", CreatureID: "owl", SourceID: "find_familiar", Status: PetDead, CurrentHP: 0})
	if err := act(t, &c, lib, func(sh Sheet) error { return PatchPet(&c, sh, lib, "o", PetPatch{CreatureID: strp("beast_land")}) }); err != nil {
		t.Fatal(err)
	}
	if p := c.Pets[0]; p.Status != PetActive || p.CurrentHP != 20 {
		t.Errorf("a new form is a new summoning: %+v, want active at 20 HP", p)
	}
}
