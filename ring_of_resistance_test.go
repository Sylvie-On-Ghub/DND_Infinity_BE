package character

// ring_of_resistance_test.go - each Ring of Resistance grants resistance to its own damage type while
// worn, with no attunement, and a resistance from two sources is listed once.

import (
	"slices"
	"testing"
)

var ringResistTypes = []DamageType{"acid", "cold", "fire", "force", "lightning", "necrotic", "poison", "psychic", "radiant", "thunder"}

func wearRings(c Character, equipped bool, types ...DamageType) Character {
	for _, d := range types {
		c.Inventory = append(c.Inventory, InventoryItem{ItemID: "ring_of_resistance_" + string(d), Quantity: 1, Equipped: equipped})
	}
	return c
}

func TestRingOfResistance(t *testing.T) {
	lib := loadExample(t)
	for _, d := range ringResistTypes {
		sh := mustCompute(t, wearRings(newChar(1, nil), true, d), lib, Ruleset{})
		if !slices.Equal(sh.Resistances, []DamageType{d}) {
			t.Errorf("%s ring: resistances = %v", d, sh.Resistances)
		}
	}

	if sh := mustCompute(t, wearRings(newChar(1, nil), false, "fire"), lib, Ruleset{}); len(sh.Resistances) != 0 {
		t.Errorf("unworn ring: resistances = %v, want none", sh.Resistances)
	}

	sh := mustCompute(t, wearRings(newChar(1, nil), true, "cold", "fire"), lib, Ruleset{})
	if !slices.Contains(sh.Resistances, "cold") || !slices.Contains(sh.Resistances, "fire") || len(sh.Resistances) != 2 {
		t.Errorf("cold and fire rings: resistances = %v", sh.Resistances)
	}
}

func TestRingOfResistanceOverlapsDragonborn(t *testing.T) {
	lib := loadElfLib(t)
	sh := mustCompute(t, wearRings(dragonbornChar(1, "brass"), true, "fire"), lib, Ruleset{})
	if !slices.Equal(sh.Resistances, []DamageType{"fire"}) {
		t.Errorf("brass dragonborn with a fire ring: resistances = %v, want fire once", sh.Resistances)
	}
}
