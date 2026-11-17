package character

// efficient_quiver_test.go - the Efficient Quiver weighs 2 lb whatever it holds. Its per-compartment
// counts are in the description only: containers limit weight, not item counts.

import "testing"

func TestEfficientQuiverContentsAreWeightless(t *testing.T) {
	lib := loadExample(t)
	sh := mustCompute(t, strChar(10,
		InventoryItem{ID: "q", ItemID: "efficient_quiver", Quantity: 1},
		InventoryItem{ItemID: "arrows_20", Quantity: 3, Container: "q"},
		InventoryItem{ItemID: "javelin", Quantity: 18, Container: "q"},
		InventoryItem{ItemID: "spear", Quantity: 4, Container: "q"},
		InventoryItem{ItemID: "longbow", Quantity: 2, Container: "q"},
	), lib, Ruleset{})
	if !approx(sh.Carry.Weight, 2) {
		t.Errorf("weight = %v, want 2 (the quiver only)", sh.Carry.Weight)
	}
	for _, e := range sh.Inventory[1:] {
		if e.Counted {
			t.Errorf("%s inside the quiver should not count", e.Name)
		}
	}
	if !(sh.Inventory[0].Contents > 0) {
		t.Errorf("the quiver should still report its contents: %v", sh.Inventory[0].Contents)
	}
	if hasWarning(sh, WarnContainerFull) {
		t.Errorf("the quiver has no weight limit: %+v", sh.Warnings)
	}
}
