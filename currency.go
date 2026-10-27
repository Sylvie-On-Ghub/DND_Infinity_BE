package character

// currency.go - coin operations on a character.
//
// Spending is exact: the named coins leave the character. If they don't have them the balance goes
// negative (the sheet warns) rather than the operation failing, in line with the rest of the engine.
// ConvertCoins breaks or combines coins when you need change. PayCoins pays a price by value from
// whatever coins are held, making change.

import (
	"errors"
	"fmt"
	"sort"
)

var (
	ErrBadConversion = errors.New("cannot convert coins")
	ErrCantAfford    = errors.New("cannot afford")
)

var defaultCurrencies = []CurrencyDef{
	{ID: "cp", Name: "Copper", Abbrev: "cp", Value: 1},
	{ID: "sp", Name: "Silver", Abbrev: "sp", Value: 10},
	{ID: "ep", Name: "Electrum", Abbrev: "ep", Value: 50},
	{ID: "gp", Name: "Gold", Abbrev: "gp", Value: 100},
	{ID: "pp", Name: "Platinum", Abbrev: "pp", Value: 1000},
}

// CurrencyDefs returns the ruleset's denominations, or the standard five.
func CurrencyDefs(rules Ruleset) []CurrencyDef {
	if len(rules.Currencies) > 0 {
		return append([]CurrencyDef(nil), rules.Currencies...)
	}
	return append([]CurrencyDef(nil), defaultCurrencies...)
}

// AddCoins adds the given amounts (negative amounts take coins away). Zero balances are removed.
func (c *Character) AddCoins(delta map[string]int) {
	if c.Coins == nil {
		c.Coins = map[string]int{}
	}
	for id, n := range delta {
		c.Coins[id] += n
		if c.Coins[id] == 0 {
			delete(c.Coins, id)
		}
	}
}

// SpendCoins removes exactly the named coins. A shortfall leaves a negative balance.
func (c *Character) SpendCoins(cost map[string]int) {
	negated := make(map[string]int, len(cost))
	for id, n := range cost {
		negated[id] = -n
	}
	c.AddCoins(negated)
}

// CoinValue is the total worth of a set of coins in base units. Unknown currencies count for nothing.
func CoinValue(coins map[string]int, defs []CurrencyDef) int {
	value := map[string]int{}
	for _, d := range defs {
		value[d.ID] = d.Value
	}
	total := 0
	for id, n := range coins {
		total += n * value[id]
	}
	return total
}

// ConvertCoins swaps amount coins of one currency for the equivalent in another, for example
// 1 pp -> 10 gp, or 10 sp -> 1 gp. The result must be a whole number of the target coin.
func ConvertCoins(c *Character, defs []CurrencyDef, from, to string, amount int) error {
	if amount <= 0 {
		return fmt.Errorf("%w: the amount must be positive", ErrBadConversion)
	}
	if from == to {
		return fmt.Errorf("%w: %q is already that currency", ErrBadConversion, from)
	}
	var f, t *CurrencyDef
	for i := range defs {
		switch defs[i].ID {
		case from:
			f = &defs[i]
		case to:
			t = &defs[i]
		}
	}
	if f == nil {
		return fmt.Errorf("%w: unknown currency %q", ErrBadConversion, from)
	}
	if t == nil {
		return fmt.Errorf("%w: unknown currency %q", ErrBadConversion, to)
	}
	if f.Value <= 0 || t.Value <= 0 {
		return fmt.Errorf("%w: a currency has no value", ErrBadConversion)
	}
	worth := amount * f.Value
	if worth%t.Value != 0 {
		return fmt.Errorf("%w: %d %s is not a whole number of %s", ErrBadConversion, amount, from, to)
	}
	c.AddCoins(map[string]int{from: -amount, to: worth / t.Value})
	return nil
}

// PayCoins pays a price by its value, drawing on whatever coins are held: 15 gp from 10 gp and
// 100 sp leaves 50 sp. Coins are used from the largest denomination down; if that leaves a
// remainder, the smallest coin that covers it is broken and the change comes back in the fewest
// coins. It returns the coins taken and the change given. Unlike SpendCoins it never goes negative:
// a price worth more than the coins held fails with ErrCantAfford and changes nothing.
func PayCoins(c *Character, defs []CurrencyDef, price map[string]int) (paid, change map[string]int, err error) {
	sorted := make([]CurrencyDef, 0, len(defs))
	for _, d := range defs {
		if d.Value > 0 {
			sorted = append(sorted, d)
		}
	}
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Value > sorted[j].Value })

	owed := CoinValue(price, defs)
	if owed <= 0 {
		return nil, nil, fmt.Errorf("%w: the price must be worth something", ErrBadConversion)
	}
	held := map[string]int{}
	for _, d := range sorted {
		if n := c.Coins[d.ID]; n > 0 {
			held[d.ID] = n
		}
	}
	if have := CoinValue(held, defs); have < owed {
		return nil, nil, fmt.Errorf("%w: the price is worth %d base units but the coins held are worth %d", ErrCantAfford, owed, have)
	}

	paid, change = map[string]int{}, map[string]int{}
	for _, d := range sorted {
		if n := min(held[d.ID], owed/d.Value); n > 0 {
			paid[d.ID] += n
			held[d.ID] -= n
			owed -= n * d.Value
		}
	}
	if owed > 0 {
		// Every denomination still held is worth more than what's owed: break the smallest.
		var coin *CurrencyDef
		for i := len(sorted) - 1; i >= 0; i-- {
			if held[sorted[i].ID] > 0 {
				coin = &sorted[i]
				break
			}
		}
		paid[coin.ID]++
		back := coin.Value - owed
		for _, d := range sorted {
			if d.Value < coin.Value && back >= d.Value {
				change[d.ID] = back / d.Value
				back %= d.Value
			}
		}
		if back != 0 {
			return nil, nil, fmt.Errorf("%w: no coin is small enough to give the change", ErrBadConversion)
		}
	}

	c.SpendCoins(paid)
	c.AddCoins(change)
	return paid, change, nil
}
