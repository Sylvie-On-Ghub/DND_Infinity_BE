package character

// inventory.go - turns a character's inventory and coins into sheet output.
//
//   - Entries resolve to library items, or to an inline Custom item.
//   - Weight: stashed entries and anything inside a stashed or "ignores contents weight"
//     container does not count. Coins weigh (by default 50 per pound) unless the ruleset says not.
//   - Carrying: capacity is Strength x 15 (ruleset-adjustable) and takes carry_capacity modifiers,
//     including multiply (Powerful Build). The default mode only reports; "variant" also applies
//     the encumbered penalties, as ordinary modifiers, so they flow through the normal pipeline.
//   - Everything is permissive: problems become warnings, never rejections.

import (
	"fmt"
	"math"
)

const (
	WarnOverCapacity     WarningCode = "over_capacity"
	WarnOverAttunement   WarningCode = "over_attunement"
	WarnBadContainer     WarningCode = "bad_container"
	WarnContainerFull    WarningCode = "container_over_capacity"
	WarnDuplicateEntryID WarningCode = "duplicate_entry_id"
	WarnUnknownCurrency  WarningCode = "unknown_currency"
	WarnNegativeCoins    WarningCode = "negative_coins"
)

func round2(f float64) float64 { return math.Round(f*100) / 100 }

// resolveInvItem finds the item for an inventory entry. An inline Custom item wins over item_id.
// Custom items without an id get one derived from the entry, so modifier sources stay distinct.
func resolveInvItem(lib Library, inv InventoryItem, index int) (Item, error) {
	if inv.Custom != nil {
		it := *inv.Custom
		if it.ID == "" {
			if inv.ID != "" {
				it.ID = "custom:" + inv.ID
			} else {
				it.ID = fmt.Sprintf("custom:%d", index)
			}
		}
		return it, nil
	}
	if inv.ItemID == "" {
		return Item{}, fmt.Errorf("inventory entry %d has neither item_id nor custom", index+1)
	}
	return lib.Item(inv.ItemID)
}

// encumbranceModifiers are the variant-rule penalties: -10 speed when encumbered; -20 speed and
// disadvantage on weapon attacks and Str/Dex/Con checks and saves when heavily encumbered.
func encumbranceModifiers(heavy bool) []Modifier {
	const src = "encumbrance"
	penalty := -10
	if heavy {
		penalty = -20
	}
	mods := []Modifier{}
	for _, t := range allSpeeds {
		mods = append(mods, Modifier{Target: SpeedTarget(t), Kind: ModAdd, Value: penalty, SourceID: src})
	}
	if heavy {
		for _, t := range []string{
			TargetAttackMelee, TargetAttackRanged,
			CheckTarget(Str), CheckTarget(Dex), CheckTarget(Con),
			SaveTarget(Str), SaveTarget(Dex), SaveTarget(Con),
		} {
			mods = append(mods, Modifier{Target: t, Kind: ModDisadvantage, SourceID: src})
		}
	}
	return mods
}

type inventoryResult struct {
	views    []InventoryView
	wallet   WalletView
	carry    CarryView
	mods     []Modifier // variant encumbrance penalties
	warnings []Warning
}

// computeInventory takes k0, a calc built from the modifiers so far, only to read the Strength score
// and carry_capacity; the encumbrance modifiers it returns are added before the final calc is built.
func computeInventory(c Character, lib Library, rules Ruleset, k0 *calc) inventoryResult {
	res := inventoryResult{views: []InventoryView{}}

	type entry struct {
		inv  InventoryItem
		item Item
		ok   bool
	}
	entries := make([]entry, len(c.Inventory))
	byID := map[string]int{}
	attuned := 0

	for i, inv := range c.Inventory {
		it, err := resolveInvItem(lib, inv, i)
		entries[i] = entry{inv: inv, item: it, ok: err == nil}
		if err != nil {
			ref := inv.ItemID
			if ref == "" {
				ref = fmt.Sprintf("entry:%d", i+1)
			}
			res.warnings = append(res.warnings, mkWarning(WarnUnknownID, ref, "", "", ref,
				fmt.Sprintf("inventory item %q could not be resolved: %v", ref, err)))
		}
		if inv.ID != "" {
			if _, dup := byID[inv.ID]; dup {
				res.warnings = append(res.warnings, mkWarning(WarnDuplicateEntryID, "", "", "", inv.ID,
					fmt.Sprintf("more than one inventory entry has the id %q", inv.ID)))
			} else {
				byID[inv.ID] = i
			}
		}
		if inv.Attuned {
			attuned++
		}
	}

	// Containment: each entry may sit inside another. Bad references and loops are cut and warned about.
	n := len(entries)
	parent := make([]int, n)
	for i, e := range entries {
		parent[i] = -1
		if e.inv.Container == "" {
			continue
		}
		p, ok := byID[e.inv.Container]
		if !ok || p == i {
			res.warnings = append(res.warnings, mkWarning(WarnBadContainer, "", "", "", e.inv.Container,
				fmt.Sprintf("inventory entry %d is stored in %q, which is not another entry's id", i+1, e.inv.Container)))
			continue
		}
		parent[i] = p
	}
	for i := range entries {
		seen := map[int]bool{i: true}
		prev := i
		for j := parent[i]; j != -1; j = parent[j] {
			if seen[j] {
				res.warnings = append(res.warnings, mkWarning(WarnBadContainer, "", "", "", fmt.Sprintf("loop:%d", prev+1),
					fmt.Sprintf("inventory entries form a containment loop; entry %d was taken out of its container", prev+1)))
				parent[prev] = -1
				break
			}
			seen[j] = true
			prev = j
		}
	}

	// Weight.
	weightOf := func(e entry) float64 {
		if !e.ok {
			return 0
		}
		return e.item.Weight * float64(e.inv.Quantity)
	}
	counted := make([]bool, n)
	contents := make([]float64, n)
	total := 0.0
	for i, e := range entries {
		counted[i] = !e.inv.Stashed
		w := weightOf(e)
		for j := parent[i]; j != -1; j = parent[j] {
			contents[j] += w
			if entries[j].inv.Stashed {
				counted[i] = false
			}
			if entries[j].ok && entries[j].item.Container != nil && entries[j].item.Container.IgnoresContentsWeight {
				counted[i] = false
			}
		}
		if counted[i] {
			total += w
		}
	}
	for i, e := range entries {
		if e.ok && e.item.Container != nil && e.item.Container.Capacity > 0 && contents[i] > e.item.Container.Capacity {
			ref := e.inv.ID
			if ref == "" {
				ref = fmt.Sprintf("entry:%d", i+1)
			}
			res.warnings = append(res.warnings, mkWarning(WarnContainerFull, "", "", "", ref,
				fmt.Sprintf("%s holds %.4g but its capacity is %.4g", e.item.Name, round2(contents[i]), e.item.Container.Capacity)))
		}
	}

	// Coins: weight, wallet view, warnings.
	perWeight := rules.CoinsPerWeight
	if perWeight == 0 {
		perWeight = 50
	}
	coinCount := 0
	for _, amt := range c.Coins {
		if amt > 0 {
			coinCount += amt
		}
	}
	if perWeight > 0 {
		total += float64(coinCount) / float64(perWeight)
	}

	defs := CurrencyDefs(rules)
	known := map[string]bool{}
	res.wallet.Coins = []CoinView{}
	for _, d := range defs {
		known[d.ID] = true
		amt := c.Coins[d.ID]
		res.wallet.Coins = append(res.wallet.Coins, CoinView{ID: d.ID, Name: d.Name, Abbrev: d.Abbrev, Amount: amt, Value: d.Value})
		res.wallet.Total += amt * d.Value
	}
	for _, id := range sortedKeys(c.Coins) {
		amt := c.Coins[id]
		if !known[id] {
			res.warnings = append(res.warnings, mkWarning(WarnUnknownCurrency, "", "", "", id,
				fmt.Sprintf("%d of currency %q, which the ruleset doesn't define, counts for nothing", amt, id)))
			res.wallet.Coins = append(res.wallet.Coins, CoinView{ID: id, Name: id, Abbrev: id, Amount: amt})
		}
		if amt < 0 {
			res.warnings = append(res.warnings, mkWarning(WarnNegativeCoins, "", "", "", id,
				fmt.Sprintf("the %s balance is %d", id, amt)))
		}
	}

	// Attunement limit.
	maxAttuned := rules.MaxAttuned
	if maxAttuned == 0 {
		maxAttuned = 3
	}
	if maxAttuned > 0 && attuned > maxAttuned {
		res.warnings = append(res.warnings, mkWarning(WarnOverAttunement, "", "", "attunement", fmt.Sprint(attuned),
			fmt.Sprintf("%d items are attuned but the limit is %d", attuned, maxAttuned)))
	}

	// Carrying capacity and encumbrance.
	mode := rules.Encumbrance
	if mode == "" {
		mode = EncumbranceCapacity
	}
	unit := rules.WeightUnit
	if unit == "" {
		unit = "lb"
	}
	res.carry = CarryView{Weight: round2(total), Unit: unit, Mode: mode}
	if mode != EncumbranceNone {
		per := rules.CarryPerStrength
		if per == 0 {
			per = 15
		}
		score := 10
		if st, ok := c.Abilities[Str]; ok {
			score = st.Score
		}
		score = k0.apply(AbilityTarget(Str), score)
		capacity := k0.apply(TargetCarryCapacity, score*per)
		if capacity < 0 {
			capacity = 0
		}
		res.carry.Capacity = capacity
		res.carry.PushDragLift = capacity * 2
		res.carry.State = CarryOK

		capF := float64(capacity)
		variant := mode == EncumbranceVariant
		heavy := variant && total > capF*2/3
		encumbered := variant && total > capF/3
		switch {
		case total > capF:
			res.carry.State = CarryOverCapacity
			res.warnings = append(res.warnings, mkWarning(WarnOverCapacity, "", "", "carry", fmt.Sprint(res.carry.Weight),
				fmt.Sprintf("carrying %.4g but the capacity is %d", res.carry.Weight, capacity)))
		case heavy:
			res.carry.State = CarryHeavilyEncumbered
		case encumbered:
			res.carry.State = CarryEncumbered
		}
		switch {
		case heavy:
			res.mods = encumbranceModifiers(true)
		case encumbered:
			res.mods = encumbranceModifiers(false)
		}
	}

	// Sheet entries.
	for i, e := range entries {
		v := InventoryView{
			EntryID: e.inv.ID, ItemID: e.inv.ItemID, Quantity: e.inv.Quantity,
			Equipped: e.inv.Equipped, Attuned: e.inv.Attuned, Stashed: e.inv.Stashed,
			Container: e.inv.Container, Counted: counted[i], Notes: e.inv.Notes, Custom: e.inv.Custom != nil,
		}
		if e.ok {
			v.Name = e.item.Name
			if v.Name == "" {
				v.Name = e.item.ID
			}
			v.Kind = e.item.Kind
			v.Weight = round2(weightOf(e))
			v.Contents = round2(contents[i])
			if e.item.Container != nil {
				cp := *e.item.Container
				v.ContainerInfo = &cp
			}
			v.Cost = e.item.Cost
			v.Rarity = e.item.Rarity
			v.Description = e.item.Description
			v.RequiresAttunement = e.item.RequiresAttunement
		} else {
			v.Name = e.inv.ItemID
			v.Unknown = true
		}
		res.views = append(res.views, v)
	}
	return res
}
