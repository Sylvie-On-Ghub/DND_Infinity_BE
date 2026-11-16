package character

// wand_of_the_war_mage_test.go - the +1/+2/+3 Wand of the War Mage: a bonus to spell attacks and the
// spell save DC. The variants share a source, so carrying two applies only the higher one.

import "testing"

func TestWandOfTheWarMage(t *testing.T) {
	lib := loadWarlockLib(t)
	caster := func(sh Sheet) SpellSourceView {
		t.Helper()
		src, ok := findSource(sh, "class:warlock")
		if !ok {
			t.Fatal("no warlock spell source")
		}
		return src
	}
	plain := caster(mustCompute(t, warlockFixture(), lib, Ruleset{}))

	holding := func(attuned bool, ids ...string) SpellSourceView {
		c := warlockFixture()
		for _, id := range ids {
			c.Inventory = append(c.Inventory, InventoryItem{ItemID: id, Quantity: 1, Equipped: true, Attuned: attuned})
		}
		return caster(mustCompute(t, c, lib, Ruleset{}))
	}
	check := func(name string, src SpellSourceView, bonus int) {
		t.Helper()
		if src.AttackBonus != plain.AttackBonus+bonus || src.SaveDC != plain.SaveDC+bonus {
			t.Errorf("%s: attack %+d, DC %d; want %+d and %d",
				name, src.AttackBonus, src.SaveDC, plain.AttackBonus+bonus, plain.SaveDC+bonus)
		}
	}

	check("+1 wand", holding(true, "wand_of_the_war_mage_1"), 1)
	check("+2 wand", holding(true, "wand_of_the_war_mage_2"), 2)
	check("+3 wand", holding(true, "wand_of_the_war_mage_3"), 3)
	check("+1 and +3 wands", holding(true, "wand_of_the_war_mage_1", "wand_of_the_war_mage_3"), 3)
	check("unattuned +3 wand", holding(false, "wand_of_the_war_mage_3"), 0)
}
