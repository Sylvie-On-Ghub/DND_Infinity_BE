package character

// cloak_of_protection_test.go - the Cloak of Protection stacks with a Ring of Protection (different
// sources), but two cloaks don't stack with each other.

import "testing"

func TestCloakOfProtectionStacksWithRing(t *testing.T) {
	lib := loadExample(t)
	base := newChar(1, nil)
	plain := mustCompute(t, base, lib, Ruleset{})

	wear := func(ids ...string) Sheet {
		c := newChar(1, nil)
		for _, id := range ids {
			c.Inventory = append(c.Inventory, InventoryItem{ItemID: id, Quantity: 1, Equipped: true, Attuned: true})
		}
		return mustCompute(t, c, lib, Ruleset{})
	}
	check := func(name string, sh Sheet, bonus int) {
		t.Helper()
		if sh.AC != plain.AC+bonus {
			t.Errorf("%s: AC %d, want %d", name, sh.AC, plain.AC+bonus)
		}
		for _, a := range []Ability{Str, Dex, Con, Int, Wis, Cha} {
			if got, want := sh.Abilities[a].SaveBonus, plain.Abilities[a].SaveBonus+bonus; got != want {
				t.Errorf("%s: %s save %d, want %d", name, a, got, want)
			}
		}
	}

	check("cloak", wear("cloak_of_protection"), 1)
	check("ring and cloak", wear("ring_of_protection", "cloak_of_protection"), 2)
	check("two cloaks", wear("cloak_of_protection", "cloak_of_protection"), 1)
}
