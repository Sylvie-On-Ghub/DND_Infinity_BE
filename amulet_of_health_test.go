package character

// amulet_of_health_test.go - the Amulet of Health raises Constitution to 19, and does nothing for a
// score that is already 19 or higher.

import "testing"

func TestAmuletOfHealth(t *testing.T) {
	lib := loadWarlockLib(t)
	wearing := func(con int, attuned bool) Sheet {
		c := warlockFixture()
		c.Abilities[Con] = AbilityScore{Score: con}
		c.Inventory = []InventoryItem{{ItemID: "amulet_of_health", Quantity: 1, Equipped: true, Attuned: attuned}}
		return mustCompute(t, c, lib, Ruleset{})
	}

	plain := wearing(14, false)
	if plain.Abilities[Con].Score != 14 {
		t.Errorf("unattuned amulet: Con %d, want 14", plain.Abilities[Con].Score)
	}

	// Con 14 (+2) becomes 19 (+4): two more hit points for each of the fixture's four levels.
	sh := wearing(14, true)
	if sh.Abilities[Con].Score != 19 || sh.Abilities[Con].Mod != 4 {
		t.Errorf("attuned amulet: Con %+v, want 19 (+4)", sh.Abilities[Con])
	}
	if sh.MaxHP != plain.MaxHP+8 {
		t.Errorf("max HP %d, want %d", sh.MaxHP, plain.MaxHP+8)
	}

	if high := wearing(20, true); high.Abilities[Con].Score != 20 {
		t.Errorf("Con 20 with the amulet = %d, want 20", high.Abilities[Con].Score)
	}
}
