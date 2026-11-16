package character

// belt_of_giant_strength_test.go - the Belt of Fire Giant Strength raises Strength to 25, and does
// nothing for a score that is already 25 or higher.

import "testing"

func TestBeltOfFireGiantStrength(t *testing.T) {
	lib := loadExample(t)
	wearing := func(str int, attuned bool) Sheet {
		return mustCompute(t, strChar(str,
			InventoryItem{ItemID: "belt_of_fire_giant_strength", Quantity: 1, Equipped: true, Attuned: attuned},
		), lib, Ruleset{})
	}

	plain := wearing(10, false)
	if plain.Abilities[Str].Score != 10 {
		t.Errorf("unattuned belt: Str %d, want 10", plain.Abilities[Str].Score)
	}

	sh := wearing(10, true)
	if sh.Abilities[Str].Score != 25 || sh.Abilities[Str].Mod != 7 {
		t.Errorf("attuned belt: Str %+v, want 25 (+7)", sh.Abilities[Str])
	}
	if sh.Carry.Capacity != plain.Carry.Capacity*25/10 {
		t.Errorf("carry capacity %d, want %d", sh.Carry.Capacity, plain.Carry.Capacity*25/10)
	}

	if high := wearing(26, true); high.Abilities[Str].Score != 26 {
		t.Errorf("Str 26 with the belt = %d, want 26", high.Abilities[Str].Score)
	}
}
