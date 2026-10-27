package character

// inventory_test.go - tests for inventory, currency and carrying. Written without being able to
// run them: a failure may be a test mistake as much as a code bug.

import (
	"errors"
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 0.0001 }

func gearLib() fakeLib {
	return fakeLib{items: map[string]Item{
		"rations":  {ID: "rations", Name: "Rations", Kind: ItemOther, Weight: 2, Cost: &Price{Amount: 5, Currency: "sp"}},
		"rope":     {ID: "rope", Name: "Rope", Kind: ItemOther, Weight: 10},
		"anvil":    {ID: "anvil", Name: "Anvil", Kind: ItemOther, Weight: 200},
		"backpack": {ID: "backpack", Name: "Backpack", Kind: ItemOther, Weight: 5, Container: &ContainerDef{Capacity: 30}},
		"holding_bag": {ID: "holding_bag", Name: "Bag of Holding", Kind: ItemOther, Weight: 15, Rarity: "uncommon",
			Container: &ContainerDef{Capacity: 500, IgnoresContentsWeight: true}},
		"ring": {ID: "ring", Name: "Ring", Kind: ItemOther, RequiresAttunement: true},
	}}
}

func strChar(str int, inv ...InventoryItem) Character {
	c := newChar(1, map[Ability]int{Str: str})
	c.Inventory = inv
	return c
}

// ---------------------------------------------------------------------------
// Weight, views, capacity
// ---------------------------------------------------------------------------

func TestInventoryWeightAndViews(t *testing.T) {
	sh := mustCompute(t, strChar(10,
		InventoryItem{ItemID: "rations", Quantity: 3},
		InventoryItem{ItemID: "rope", Quantity: 1},
	), gearLib(), Ruleset{})

	if !approx(sh.Carry.Weight, 16) || sh.Carry.Capacity != 150 || sh.Carry.PushDragLift != 300 || sh.Carry.State != CarryOK {
		t.Errorf("carry = %+v, want 16 of 150, push/drag/lift 300, ok", sh.Carry)
	}
	if sh.Carry.Unit != "lb" || sh.Carry.Mode != EncumbranceCapacity {
		t.Errorf("carry unit/mode = %q/%q, want lb/capacity", sh.Carry.Unit, sh.Carry.Mode)
	}
	if len(sh.Inventory) != 2 {
		t.Fatalf("inventory = %+v", sh.Inventory)
	}
	r := sh.Inventory[0]
	if r.Name != "Rations" || !approx(r.Weight, 6) || !r.Counted || r.Cost == nil || r.Cost.Amount != 5 {
		t.Errorf("rations view = %+v", r)
	}
}

func TestStashedItemsDoNotCount(t *testing.T) {
	sh := mustCompute(t, strChar(10,
		InventoryItem{ItemID: "rope", Quantity: 1},
		InventoryItem{ItemID: "anvil", Quantity: 1, Stashed: true},
	), gearLib(), Ruleset{})
	if !approx(sh.Carry.Weight, 10) {
		t.Errorf("weight = %v, want 10", sh.Carry.Weight)
	}
	if sh.Inventory[1].Counted {
		t.Error("the stashed anvil should not count")
	}
	if hasWarning(sh, WarnOverCapacity) {
		t.Error("a stashed anvil should not cause an over capacity warning")
	}
}

func TestContainersCountTheirContents(t *testing.T) {
	sh := mustCompute(t, strChar(10,
		InventoryItem{ID: "bp", ItemID: "backpack", Quantity: 1},
		InventoryItem{ItemID: "rations", Quantity: 3, Container: "bp"},
	), gearLib(), Ruleset{})
	if !approx(sh.Carry.Weight, 11) { // 5 + 3x2
		t.Errorf("weight = %v, want 11", sh.Carry.Weight)
	}
	if !approx(sh.Inventory[0].Contents, 6) {
		t.Errorf("backpack contents = %v, want 6", sh.Inventory[0].Contents)
	}
}

func TestBagOfHoldingContentsAreWeightless(t *testing.T) {
	sh := mustCompute(t, strChar(10,
		InventoryItem{ID: "bag", ItemID: "holding_bag", Quantity: 1},
		InventoryItem{ItemID: "anvil", Quantity: 1, Container: "bag"},
	), gearLib(), Ruleset{})
	if !approx(sh.Carry.Weight, 15) {
		t.Errorf("weight = %v, want 15 (the bag only)", sh.Carry.Weight)
	}
	if sh.Inventory[1].Counted {
		t.Error("the anvil inside the bag should not count")
	}
	if !approx(sh.Inventory[0].Contents, 200) {
		t.Errorf("the bag still reports its contents: %v", sh.Inventory[0].Contents)
	}
}

func TestStashedContainerStashesItsContents(t *testing.T) {
	sh := mustCompute(t, strChar(10,
		InventoryItem{ID: "bp", ItemID: "backpack", Quantity: 1, Stashed: true},
		InventoryItem{ItemID: "rope", Quantity: 2, Container: "bp"},
	), gearLib(), Ruleset{})
	if !approx(sh.Carry.Weight, 0) {
		t.Errorf("weight = %v, want 0", sh.Carry.Weight)
	}
}

func TestContainerOverCapacityWarns(t *testing.T) {
	sh := mustCompute(t, strChar(10,
		InventoryItem{ID: "bp", ItemID: "backpack", Quantity: 1},
		InventoryItem{ItemID: "rope", Quantity: 4, Container: "bp"}, // 40 in a 30 capacity pack
	), gearLib(), Ruleset{})
	if !hasWarning(sh, WarnContainerFull) {
		t.Errorf("expected container_over_capacity, got %+v", sh.Warnings)
	}
}

func TestBadContainerReferencesWarn(t *testing.T) {
	cases := map[string][]InventoryItem{
		"dangling": {{ItemID: "rope", Quantity: 1, Container: "nope"}},
		"itself":   {{ID: "a", ItemID: "backpack", Quantity: 1, Container: "a"}},
		"loop": {
			{ID: "a", ItemID: "backpack", Quantity: 1, Container: "b"},
			{ID: "b", ItemID: "backpack", Quantity: 1, Container: "a"},
		},
	}
	for name, inv := range cases {
		t.Run(name, func(t *testing.T) {
			sh := mustCompute(t, strChar(10, inv...), gearLib(), Ruleset{}) // must terminate
			if !hasWarning(sh, WarnBadContainer) {
				t.Errorf("expected bad_container, got %+v", sh.Warnings)
			}
		})
	}
}

func TestDuplicateEntryIDWarns(t *testing.T) {
	sh := mustCompute(t, strChar(10,
		InventoryItem{ID: "x", ItemID: "rope", Quantity: 1},
		InventoryItem{ID: "x", ItemID: "rope", Quantity: 1},
	), gearLib(), Ruleset{})
	if !hasWarning(sh, WarnDuplicateEntryID) {
		t.Errorf("expected duplicate_entry_id, got %+v", sh.Warnings)
	}
}

func TestUnknownInventoryItemIsFlagged(t *testing.T) {
	sh := mustCompute(t, strChar(10, InventoryItem{ItemID: "mystery", Quantity: 1}), gearLib(), Ruleset{})
	if !hasWarningValue(sh, WarnUnknownID, "mystery") {
		t.Errorf("expected unknown_id for mystery, got %+v", sh.Warnings)
	}
	if len(sh.Inventory) != 1 || !sh.Inventory[0].Unknown || sh.Inventory[0].Name != "mystery" {
		t.Errorf("view = %+v, want an unknown entry named after the id", sh.Inventory)
	}
}

func TestOverCapacityWarnsOnly(t *testing.T) {
	sh := mustCompute(t, strChar(10, InventoryItem{ItemID: "anvil", Quantity: 1}), gearLib(), Ruleset{})
	if sh.Carry.State != CarryOverCapacity || !hasWarning(sh, WarnOverCapacity) {
		t.Errorf("carry = %+v, want over_capacity with a warning", sh.Carry)
	}
}

func TestCapacityFollowsStrengthAndModifiers(t *testing.T) {
	if got := mustCompute(t, strChar(16), gearLib(), Ruleset{}).Carry.Capacity; got != 240 {
		t.Errorf("Str 16 capacity = %d, want 240", got)
	}
	if got := mustCompute(t, strChar(10), gearLib(), Ruleset{CarryPerStrength: 8}).Carry.Capacity; got != 80 {
		t.Errorf("carry_per_strength 8: capacity = %d, want 80", got)
	}

	c := strChar(10)
	c.Modifiers = []Modifier{{Target: TargetCarryCapacity, Kind: ModMultiply, Value: 2, SourceID: "powerful_build"}}
	sh := mustCompute(t, c, gearLib(), Ruleset{})
	if sh.Carry.Capacity != 300 || sh.Carry.PushDragLift != 600 {
		t.Errorf("powerful build: capacity %d, push/drag/lift %d; want 300 and 600", sh.Carry.Capacity, sh.Carry.PushDragLift)
	}
}

func TestMultiplyOrderAppliesAfterAddBeforeMin(t *testing.T) {
	cases := []struct {
		name string
		min  int
		want int
	}{
		{"no floor", 0, 320}, // (150 + 10) x 2
		{"floor above", 400, 400},
		{"floor below", 100, 320},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := strChar(10)
			c.Modifiers = []Modifier{
				{Target: TargetCarryCapacity, Kind: ModAdd, Value: 10, SourceID: "a"},
				{Target: TargetCarryCapacity, Kind: ModMultiply, Value: 2, SourceID: "b"},
			}
			if tc.min > 0 {
				c.Modifiers = append(c.Modifiers, Modifier{Target: TargetCarryCapacity, Kind: ModMin, Value: tc.min, SourceID: "c"})
			}
			if got := mustCompute(t, c, gearLib(), Ruleset{}).Carry.Capacity; got != tc.want {
				t.Errorf("capacity = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestEncumbranceNoneSkipsCapacity(t *testing.T) {
	sh := mustCompute(t, strChar(10, InventoryItem{ItemID: "anvil", Quantity: 1}), gearLib(), Ruleset{Encumbrance: EncumbranceNone})
	if sh.Carry.Capacity != 0 || sh.Carry.State != "" || hasWarning(sh, WarnOverCapacity) {
		t.Errorf("carry = %+v, want weight only", sh.Carry)
	}
	if !approx(sh.Carry.Weight, 200) {
		t.Errorf("weight = %v, want 200", sh.Carry.Weight)
	}
}

// ---------------------------------------------------------------------------
// Variant encumbrance
// ---------------------------------------------------------------------------

func walkingChar(str int, inv ...InventoryItem) Character {
	c := strChar(str, inv...)
	c.Modifiers = []Modifier{{Target: SpeedTarget(SpeedWalk), Kind: ModAdd, Value: 30, SourceID: "race"}}
	return c
}

func TestVariantEncumbrance(t *testing.T) {
	variant := Ruleset{Encumbrance: EncumbranceVariant}

	light := mustCompute(t, walkingChar(10, InventoryItem{ItemID: "rope", Quantity: 4}), gearLib(), variant) // 40 of 150
	if light.Carry.State != CarryOK || light.Speeds[SpeedWalk] != 30 {
		t.Errorf("light load: state %q, speed %d; want ok and 30", light.Carry.State, light.Speeds[SpeedWalk])
	}

	enc := mustCompute(t, walkingChar(10, InventoryItem{ItemID: "rope", Quantity: 6}), gearLib(), variant) // 60: over 50
	if enc.Carry.State != CarryEncumbered || enc.Speeds[SpeedWalk] != 20 {
		t.Errorf("encumbered: state %q, speed %d; want encumbered and 20", enc.Carry.State, enc.Speeds[SpeedWalk])
	}
	if enc.Skills["athletics"].Disadvantage {
		t.Error("plain encumbrance should not impose disadvantage")
	}

	heavy := mustCompute(t, walkingChar(10, InventoryItem{ItemID: "rope", Quantity: 12}), gearLib(), variant) // 120: over 100
	if heavy.Carry.State != CarryHeavilyEncumbered || heavy.Speeds[SpeedWalk] != 10 {
		t.Errorf("heavily encumbered: state %q, speed %d; want heavily_encumbered and 10", heavy.Carry.State, heavy.Speeds[SpeedWalk])
	}
	if !heavy.Skills["athletics"].Disadvantage || !heavy.Skills["stealth"].Disadvantage {
		t.Error("Str and Dex skills should have disadvantage when heavily encumbered")
	}
	if heavy.Skills["arcana"].Disadvantage {
		t.Error("Int skills should be unaffected")
	}
	if a, _ := findAttack(heavy, "Unarmed Strike"); !a.Disadvantage {
		t.Error("weapon attacks should have disadvantage when heavily encumbered")
	}
}

func TestDefaultModeHasNoPenalties(t *testing.T) {
	sh := mustCompute(t, walkingChar(10, InventoryItem{ItemID: "rope", Quantity: 12}), gearLib(), Ruleset{})
	if sh.Speeds[SpeedWalk] != 30 || sh.Skills["athletics"].Disadvantage {
		t.Errorf("capacity mode must not change speed or rolls: speed %d", sh.Speeds[SpeedWalk])
	}
}

// ---------------------------------------------------------------------------
// Coins and the wallet
// ---------------------------------------------------------------------------

func TestWalletTotalsAndCoinWeight(t *testing.T) {
	c := strChar(10)
	c.Coins = map[string]int{"gp": 100, "sp": 5, "cp": 3}
	sh := mustCompute(t, c, gearLib(), Ruleset{})

	if sh.Wallet.Total != 10053 { // 10000 + 50 + 3
		t.Errorf("wallet total = %d, want 10053", sh.Wallet.Total)
	}
	if len(sh.Wallet.Coins) != 5 || sh.Wallet.Coins[3].ID != "gp" || sh.Wallet.Coins[3].Amount != 100 {
		t.Errorf("wallet coins = %+v, want the five standard denominations with 100 gp", sh.Wallet.Coins)
	}
	if !approx(sh.Carry.Weight, 2.16) { // 108 coins at 50 per pound
		t.Errorf("weight = %v, want 2.16", sh.Carry.Weight)
	}
}

func TestCoinsCanWeighNothing(t *testing.T) {
	c := strChar(10)
	c.Coins = map[string]int{"gp": 5000}
	sh := mustCompute(t, c, gearLib(), Ruleset{CoinsPerWeight: -1})
	if !approx(sh.Carry.Weight, 0) {
		t.Errorf("weight = %v, want 0", sh.Carry.Weight)
	}
}

func TestUnknownAndNegativeCoinsWarn(t *testing.T) {
	c := strChar(10)
	c.Coins = map[string]int{"zorkmid": 3, "gp": -2}
	sh := mustCompute(t, c, gearLib(), Ruleset{})
	if !hasWarning(sh, WarnUnknownCurrency) || !hasWarning(sh, WarnNegativeCoins) {
		t.Errorf("expected unknown_currency and negative_coins, got %+v", sh.Warnings)
	}
	if sh.Wallet.Total != -200 {
		t.Errorf("total = %d, want -200 (unknown coins count for nothing)", sh.Wallet.Total)
	}
}

func TestCustomCurrenciesFromRuleset(t *testing.T) {
	rules := Ruleset{Currencies: []CurrencyDef{
		{ID: "crown", Name: "Crown", Abbrev: "cr", Value: 1},
		{ID: "mark", Name: "Mark", Abbrev: "mk", Value: 20},
	}}
	c := strChar(10)
	c.Coins = map[string]int{"crown": 5, "mark": 2}
	sh := mustCompute(t, c, gearLib(), rules)
	if sh.Wallet.Total != 45 || len(sh.Wallet.Coins) != 2 {
		t.Errorf("wallet = %+v, want 2 denominations worth 45", sh.Wallet)
	}
	if hasWarning(sh, WarnUnknownCurrency) {
		t.Error("ruleset currencies should be recognised")
	}
}

func TestCoinOperations(t *testing.T) {
	var c Character
	c.AddCoins(map[string]int{"gp": 10, "sp": 4})
	c.SpendCoins(map[string]int{"gp": 3})
	if c.Coins["gp"] != 7 || c.Coins["sp"] != 4 {
		t.Errorf("coins = %v, want 7 gp and 4 sp", c.Coins)
	}

	c.SpendCoins(map[string]int{"sp": 4})
	if _, present := c.Coins["sp"]; present {
		t.Error("a zero balance should be removed")
	}

	c.SpendCoins(map[string]int{"gp": 10}) // only 7: goes negative instead of failing
	if c.Coins["gp"] != -3 {
		t.Errorf("gp = %d, want -3", c.Coins["gp"])
	}
}

func TestConvertCoins(t *testing.T) {
	defs := CurrencyDefs(Ruleset{})

	var c Character
	c.AddCoins(map[string]int{"pp": 2, "sp": 25})
	if err := ConvertCoins(&c, defs, "pp", "gp", 1); err != nil {
		t.Fatalf("pp to gp: %v", err)
	}
	if c.Coins["pp"] != 1 || c.Coins["gp"] != 10 {
		t.Errorf("coins = %v, want 1 pp and 10 gp", c.Coins)
	}
	if err := ConvertCoins(&c, defs, "sp", "gp", 20); err != nil {
		t.Fatalf("sp to gp: %v", err)
	}
	if c.Coins["sp"] != 5 || c.Coins["gp"] != 12 {
		t.Errorf("coins = %v, want 5 sp and 12 gp", c.Coins)
	}

	before := CoinValue(c.Coins, defs)
	if before != CoinValue(map[string]int{"pp": 1, "gp": 12, "sp": 5}, defs) {
		t.Error("conversion must preserve total value")
	}

	for _, bad := range []struct {
		from, to string
		amount   int
	}{
		{"sp", "gp", 5},  // 50 cp is not a whole gp
		{"gp", "gp", 1},  // same currency
		{"gp", "xx", 1},  // unknown target
		{"xx", "gp", 1},  // unknown source
		{"gp", "sp", 0},  // not positive
		{"gp", "sp", -3}, // not positive
	} {
		if err := ConvertCoins(&c, defs, bad.from, bad.to, bad.amount); !errors.Is(err, ErrBadConversion) {
			t.Errorf("convert %d %s to %s: err = %v, want ErrBadConversion", bad.amount, bad.from, bad.to, err)
		}
	}
	if CoinValue(c.Coins, defs) != before {
		t.Error("failed conversions must not change the coins")
	}
}

// ---------------------------------------------------------------------------
// Attunement and custom items
// ---------------------------------------------------------------------------

func TestAttunementLimit(t *testing.T) {
	four := strChar(10,
		InventoryItem{ItemID: "ring", Quantity: 1, Attuned: true},
		InventoryItem{ItemID: "ring", Quantity: 1, Attuned: true},
		InventoryItem{ItemID: "ring", Quantity: 1, Attuned: true},
		InventoryItem{ItemID: "ring", Quantity: 1, Attuned: true},
	)
	if !hasWarning(mustCompute(t, four, gearLib(), Ruleset{}), WarnOverAttunement) {
		t.Error("four attuned items should exceed the default limit of 3")
	}
	if hasWarning(mustCompute(t, four, gearLib(), Ruleset{MaxAttuned: 5}), WarnOverAttunement) {
		t.Error("a limit of 5 should allow four")
	}
	if hasWarning(mustCompute(t, four, gearLib(), Ruleset{MaxAttuned: -1}), WarnOverAttunement) {
		t.Error("a negative limit means unlimited")
	}
	three := strChar(10, four.Inventory[:3]...)
	if hasWarning(mustCompute(t, three, gearLib(), Ruleset{}), WarnOverAttunement) {
		t.Error("three attuned items is within the limit")
	}
}

func TestCustomItemEntry(t *testing.T) {
	c := strChar(10, InventoryItem{
		Quantity: 1, Equipped: true,
		Custom: &Item{
			Name: "Strange Charm", Kind: ItemOther, Weight: 0.5,
			Modifiers: []Modifier{{Target: TargetAC, Kind: ModAdd, Value: 1}},
		},
	})
	sh := mustCompute(t, c, gearLib(), Ruleset{})

	if sh.AC != 11 { // 10 + Dex 0 + the charm
		t.Errorf("AC = %d, want 11", sh.AC)
	}
	if len(sh.Inventory) != 1 || sh.Inventory[0].Name != "Strange Charm" || !sh.Inventory[0].Custom {
		t.Errorf("inventory = %+v, want the custom charm", sh.Inventory)
	}
	if !approx(sh.Carry.Weight, 0.5) {
		t.Errorf("weight = %v, want 0.5", sh.Carry.Weight)
	}
	if hasWarning(sh, WarnUnknownID) && hasWarningValue(sh, WarnUnknownID, "entry:1") {
		t.Error("a custom item should not be reported as unresolved")
	}
}

func TestEntryWithNothingToResolveWarns(t *testing.T) {
	sh := mustCompute(t, strChar(10, InventoryItem{Quantity: 1}), gearLib(), Ruleset{})
	if !hasWarningValue(sh, WarnUnknownID, "entry:1") {
		t.Errorf("expected an unresolved entry warning, got %+v", sh.Warnings)
	}
}

// ---------------------------------------------------------------------------
// Loader validation for the new fields
// ---------------------------------------------------------------------------

func TestLoaderValidatesInventoryFields(t *testing.T) {
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"items/i.json":    `{"id":"i","name":"I","kind":"other","weight":-1,"cost":{"amount":-2,"currency":"gp"},"container":{"capacity":-5}}`,
		"features/f.json": `{"id":"f","name":"F","origin":"feat","modifiers":[{"target":"carry_capacity","kind":"multiply","value":0}]}`,
	}))
	for _, want := range []string{"weight cannot be negative", "cost cannot be negative", "container capacity cannot be negative", "multiply needs"} {
		if !hasIssue(lib, SevError, want) {
			t.Errorf("expected an error containing %q, got %v", want, lib.Issues())
		}
	}
}

func TestLoaderAcceptsValidMultiplyAndGear(t *testing.T) {
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"items/i.json": `{"id":"i","name":"I","kind":"other","weight":2.5,"rarity":"rare",
			"cost":{"amount":50,"currency":"gp"},"container":{"capacity":30,"ignores_contents_weight":true}}`,
		"features/f.json": `{"id":"f","name":"F","origin":"race","modifiers":[{"target":"carry_capacity","kind":"multiply","value":2}]}`,
	}))
	if lib.HasErrors() {
		dumpIssues(t, lib)
		t.Error("valid gear and multiply should load without errors")
	}
}

// ---------------------------------------------------------------------------
// The example pack's gear, end to end
// ---------------------------------------------------------------------------

func TestExampleGearEndToEnd(t *testing.T) {
	lib := loadExample(t)
	c := strChar(10,
		InventoryItem{ID: "bp", ItemID: "backpack", Quantity: 1},
		InventoryItem{ItemID: "rations", Quantity: 5, Container: "bp"},
		InventoryItem{ID: "bag", ItemID: "bag_of_holding", Quantity: 1},
		InventoryItem{ItemID: "rope_hempen", Quantity: 3, Container: "bag"},
	)
	c.Coins = map[string]int{"gp": 50}
	c.FeatureIDs = []string{"powerful_build"}
	c.Classes = nil // strChar uses the placeholder class "test", which would raise its own unknown_id
	sh := mustCompute(t, c, lib, Ruleset{})

	// backpack 5 + rations 10 + bag 15 (its rope is weightless) + 50 coins at 50 per pound = 31
	if !approx(sh.Carry.Weight, 31) {
		t.Errorf("weight = %v, want 31", sh.Carry.Weight)
	}
	if sh.Carry.Capacity != 300 { // Str 10 x 15, doubled by Powerful Build
		t.Errorf("capacity = %d, want 300", sh.Carry.Capacity)
	}
	for _, w := range sh.Warnings {
		switch w.Code {
		case WarnUnknownID, WarnBadContainer, WarnContainerFull, WarnUnknownTarget, WarnOverCapacity:
			t.Errorf("unexpected warning %s: %s", w.Code, w.Message)
		}
	}
}

func TestContainerInfoOnViews(t *testing.T) {
	sh := mustCompute(t, strChar(10,
		InventoryItem{ID: "bp", ItemID: "backpack", Quantity: 1},
		InventoryItem{ItemID: "rope", Quantity: 1, Container: "bp"},
		InventoryItem{ID: "box", Quantity: 1, Custom: &Item{Name: "Empty Box", Kind: ItemOther, Container: &ContainerDef{}}},
	), gearLib(), Ruleset{})

	bp, rope, box := sh.Inventory[0], sh.Inventory[1], sh.Inventory[2]
	if bp.ContainerInfo == nil || bp.ContainerInfo.Capacity != 30 || !approx(bp.Contents, 10) {
		t.Errorf("backpack view = %+v, want container_info capacity 30 holding 10", bp)
	}
	if rope.ContainerInfo != nil {
		t.Errorf("rope is not a container: %+v", rope.ContainerInfo)
	}
	if box.ContainerInfo == nil || box.Contents != 0 {
		t.Errorf("an empty custom container should still carry container_info: %+v", box)
	}
}

func TestItemListSummaryHasDetails(t *testing.T) {
	lib, err := LoadDirs(LoadOptions{Strict: true}, "content/example")
	if err != nil {
		t.Fatal(err)
	}
	items, err := lib.List("items")
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.ID == "backpack" {
			if it.Kind != ItemOther || it.Weight != 5 || it.Cost == nil {
				t.Errorf("backpack summary = %+v, want kind, weight and cost", it)
			}
			return
		}
	}
	t.Errorf("backpack missing from the item list: %+v", items)
}
