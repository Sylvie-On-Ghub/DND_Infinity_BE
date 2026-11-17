package character

// staff_of_power_test.go - the Staff of Power: a +2 quarterstaff that also gives +2 to AC, saves and
// spell attacks, and casts its spells with the wielder's spell save DC from 20 charges.

import "testing"

func TestStaffOfPower(t *testing.T) {
	lib := loadWarlockLib(t)
	holding := func(attuned bool) Sheet {
		c := warlockFixture()
		c.Inventory = []InventoryItem{{ItemID: "staff_of_power", Quantity: 1, Equipped: true, Attuned: attuned}}
		return mustCompute(t, c, lib, Ruleset{})
	}
	plain := mustCompute(t, warlockFixture(), lib, Ruleset{})
	sh := holding(true)
	noContentWarnings(t, sh)

	if sh.AC != plain.AC+2 {
		t.Errorf("AC %d, want %d", sh.AC, plain.AC+2)
	}
	for _, a := range []Ability{Str, Dex, Con, Int, Wis, Cha} {
		if got, want := sh.Abilities[a].SaveBonus, plain.Abilities[a].SaveBonus+2; got != want {
			t.Errorf("%s save %d, want %d", a, got, want)
		}
	}
	pact, _ := findSource(plain, "class:warlock")
	if src, _ := findSource(sh, "class:warlock"); src.AttackBonus != pact.AttackBonus+2 || src.SaveDC != pact.SaveDC {
		t.Errorf("warlock spells: attack %+d DC %d, want %+d and an unchanged %d", src.AttackBonus, src.SaveDC, pact.AttackBonus+2, pact.SaveDC)
	}

	// The quarterstaff: Str 8 (-1) + PB 2 + 2 to hit, 1d6 - 1 + 2 damage; the warlock is proficient with simple weapons.
	a, ok := findAttack(sh, "Staff of Power")
	if !ok || a.ToHit != 3 || a.DamageDice != "1d6" || a.DamageBonus != 1 || a.VersatileDice != "1d8" {
		t.Errorf("staff attack = %+v (found %v), want +3 to hit, 1d6+1, versatile 1d8", a, ok)
	}

	// Its spells use the warlock's Charisma and spell save DC, and the +2 to spell attacks.
	src, ok := findSource(sh, "item:staff_of_power")
	if !ok || src.Ability != Cha || src.SaveDC != pact.SaveDC || src.AttackBonus != pact.AttackBonus+2 || len(src.Spells) != 9 {
		t.Errorf("staff spells = %+v (found %v), want Cha, DC %d, attack %+d, 9 spells", src, ok, pact.SaveDC, pact.AttackBonus+2)
	}
	if r, ok := findResource(sh, "item:staff_of_power"); !ok || r.Max != 20 || r.Recharge != RechargeDawn {
		t.Errorf("charges = %+v (found %v), want 20 back at dawn", r, ok)
	}
	if a, ok := actionNamed(sh, "Retributive Strike"); !ok || a.Type != ActionAction {
		t.Errorf("retributive strike = %+v (found %v)", a, ok)
	}

	// Unattuned, only the plain quarterstaff is left.
	off := holding(false)
	if off.AC != plain.AC {
		t.Errorf("unattuned staff: AC %d, want %d", off.AC, plain.AC)
	}
	if _, ok := findSource(off, "item:staff_of_power"); ok {
		t.Error("unattuned staff shouldn't cast spells")
	}
}
