package character

// mage_armor_test.go - a castable spell with an AC formula (Mage Armor) can be chosen as the AC formula.

import "testing"

// mageArmorFixture is the level 4 warlock (Dex 14) without Armor of Shadows, so Mage Armor comes
// only from where each test puts it.
func mageArmorFixture() Character {
	c := warlockFixture()
	c.Selections[1].Picked = []string{"devils_sight", "beguiling_influence"}
	return c
}

func acOption(sh Sheet, id string) (ACOption, bool) {
	for _, o := range sh.ACDetail.Options {
		if o.ID == id {
			return o, true
		}
	}
	return ACOption{}, false
}

func TestMageArmorKnownCanBeChosenAsAC(t *testing.T) {
	lib := loadWarlockLib(t)
	c := mageArmorFixture()
	sp := c.Spellbooks["class:warlock"]
	sp.Known = append(sp.Known[:4:4], "mage_armor")
	c.Spellbooks["class:warlock"] = sp

	// Offered, but not picked on its own: the sheet can't tell whether it has been cast.
	sh := mustCompute(t, c, lib, Ruleset{})
	o, ok := acOption(sh, "mage_armor")
	if !ok || !o.FromSpell || !o.Eligible || o.Value != 15 {
		t.Fatalf("mage armor option = %+v (found %v), want an eligible spell option worth 15", o, ok)
	}
	if sh.ACDetail.FormulaID != "base" || sh.AC != 12 {
		t.Errorf("AC = %d via %s, want 12 via base until mage armor is chosen", sh.AC, sh.ACDetail.FormulaID)
	}

	c.ACFormulaOverride = "mage_armor"
	sh = mustCompute(t, c, lib, Ruleset{})
	if sh.ACDetail.FormulaID != "mage_armor" || sh.AC != 15 {
		t.Errorf("AC = %d via %s, want 15 via mage_armor", sh.AC, sh.ACDetail.FormulaID)
	}
}

func TestMageArmorFromArmorOfShadows(t *testing.T) {
	lib := loadWarlockLib(t)
	c := warlockFixture() // has Armor of Shadows
	c.ACFormulaOverride = "mage_armor"
	sh := mustCompute(t, c, lib, Ruleset{})
	if sh.ACDetail.FormulaID != "mage_armor" || sh.AC != 15 {
		t.Errorf("AC = %d via %s, want 15 via mage_armor", sh.AC, sh.ACDetail.FormulaID)
	}
}

func TestMageArmorNeedsTheSpell(t *testing.T) {
	lib := loadWarlockLib(t)
	c := mageArmorFixture()
	c.ACFormulaOverride = "mage_armor"
	sh := mustCompute(t, c, lib, Ruleset{})
	if _, ok := acOption(sh, "mage_armor"); ok {
		t.Error("mage armor should not be offered without the spell")
	}
	if sh.ACDetail.FormulaID == "mage_armor" || !hasWarningValue(sh, WarnUnknownID, "mage_armor") {
		t.Errorf("override without the spell should warn and fall back; got %s, warnings %+v", sh.ACDetail.FormulaID, sh.Warnings)
	}
}

func TestMageArmorWithArmourWornWarns(t *testing.T) {
	lib := loadWarlockLib(t)
	c := warlockFixture()
	c.ACFormulaOverride = "mage_armor"
	c.Inventory = []InventoryItem{{ItemID: "leather_armor", Quantity: 1, Equipped: true}}
	sh := mustCompute(t, c, lib, Ruleset{})
	if !hasWarningValue(sh, WarnACRequirementUnmet, "mage_armor") {
		t.Errorf("forcing mage armor while wearing armour should warn: %+v", sh.Warnings)
	}
}
