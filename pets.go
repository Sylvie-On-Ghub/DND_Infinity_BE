package character

// pets.go - computing pets onto the sheet.
//
// A pet's stat block comes from its library Creature. Creature formulas are evaluated with the owner's
// finished sheet as context (owner.* terms), so a Primal Companion grows with its ranger. Modifiers on a
// pet come from, in order: the creature itself, the owner's pet.<tag>.* modifiers for a tag the creature
// has, and the pet's own ad hoc modifiers. Rules problems are warnings on the pet, never errors.

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// petView computes one pet against its owner's finished sheet.
func petView(p Pet, c Character, sh Sheet, lib Library, skillDefs []SkillDef) PetView {
	v := PetView{
		ID: p.ID, Name: p.Name, CreatureID: p.CreatureID, SourceID: p.SourceID, Status: p.Status,
		CurrentHP: p.CurrentHP, TempHP: p.TempHP,
		Abilities: map[Ability]AbilityView{}, Speeds: map[SpeedType]int{}, Senses: []Sense{},
		Skills: map[string]SkillView{}, Resistances: []DamageType{}, Immunities: []DamageType{},
		Languages: []string{}, Traits: []Trait{}, Attacks: []PetAttackView{}, Actions: []Action{},
		Conditions: append([]Condition{}, p.Conditions...), Notes: []Modifier{}, Warnings: []Warning{},
	}
	cr, err := lib.Creature(p.CreatureID)
	if err != nil {
		v.Warnings = append(v.Warnings, mkWarning(WarnUnknownID, p.ID, "", "", p.CreatureID,
			fmt.Sprintf("pet %q is a %q, which isn't in the library", petLabel(p), p.CreatureID)))
		return v
	}
	v.CreatureName, v.Size, v.Type = cr.Name, cr.Size, cr.Type
	v.Languages = append(v.Languages, cr.Languages...)
	v.Traits = append(v.Traits, cr.Traits...)
	v.Actions = append(v.Actions, cr.Actions...)

	owner := ownerContext(c, sh, lib, p.SourceID)
	var badTerms []string
	eval := func(f Formula) int {
		n, bad := evalFormula(f, formulaCtx{modOf: map[Ability]int{}, owner: &owner})
		badTerms = append(badTerms, bad...)
		return n
	}

	// The pet's own scores and proficiency bonus come first: its other formulas read them.
	modOf := map[Ability]int{}
	for _, a := range []Ability{Str, Dex, Con, Int, Wis, Cha} {
		score, ok := cr.Abilities[a]
		if !ok {
			score = 10
		}
		modOf[a] = floorMod(score)
	}
	pb := 2
	if cr.PB != nil {
		pb = eval(*cr.PB)
	}
	ctx := formulaCtx{modOf: modOf, pb: pb, owner: &owner}
	eval = func(f Formula) int {
		n, bad := evalFormula(f, ctx)
		badTerms = append(badTerms, bad...)
		return n
	}
	v.PB = pb

	mods := append([]Modifier{}, cr.Modifiers...)
	for _, m := range sh.petMods {
		tag, inner, _ := splitPetTarget(m.Target)
		if slices.Contains(cr.Tags, tag) {
			m.Target = inner
			m.Note = fillOwnerPlaceholders(m.Note, owner)
			mods = append(mods, m)
		}
	}
	mods = dedupe(append(mods, p.Modifiers...))
	k := newCalc(mods)

	for _, a := range []Ability{Str, Dex, Con, Int, Wis, Cha} {
		score := cr.Abilities[a]
		if score == 0 {
			score = 10
		}
		prof := slices.Contains(cr.Saves, a)
		save := modOf[a] + k.sum(SaveTarget(a), TargetSaveAll)
		if prof {
			save += pb
		}
		v.Abilities[a] = AbilityView{Score: score, Mod: modOf[a], SaveBonus: save, SaveProf: prof}
	}

	v.AC = k.apply(TargetAC, eval(cr.AC))
	v.MaxHP = k.apply(TargetMaxHP, eval(cr.HP))

	for _, s := range []SpeedType{SpeedWalk, SpeedFly, SpeedSwim, SpeedClimb, SpeedBurrow} {
		if n := k.apply(SpeedTarget(s), cr.Speeds[s]); n > 0 {
			v.Speeds[s] = n
		}
	}
	for _, s := range cr.Senses {
		v.Senses = append(v.Senses, Sense{Name: s.Name, Range: k.apply(SenseTarget(s.Name), s.Range)})
	}

	abilityOf := map[string]Ability{}
	for _, sd := range skillDefs {
		abilityOf[sd.ID] = sd.DefaultAbility
	}
	for _, id := range sortedKeys(cr.Skills) {
		lvl := cr.Skills[id]
		a := abilityOf[id]
		bonus := modOf[a] + k.sum(SkillTarget(id), CheckTarget(a), TargetCheckAll)
		switch lvl {
		case ProfTrained:
			bonus += pb
		case ProfExpert:
			bonus += 2 * pb
		case ProfHalf:
			bonus += pb / 2
		}
		v.Skills[id] = SkillView{Ability: a, Level: lvl, Bonus: bonus, Passive: 10 + bonus,
			Advantage:    k.flag(ModAdvantage, SkillTarget(id), CheckTarget(a), TargetCheckAll),
			Disadvantage: k.flag(ModDisadvantage, SkillTarget(id), CheckTarget(a), TargetCheckAll)}
	}

	v.Resistances = mergeDamageTypes(cr.Resistances, k.damageList("resist."))
	v.Immunities = mergeDamageTypes(cr.Immunities, k.damageList("immune."))

	for _, a := range cr.Attacks {
		v.Attacks = append(v.Attacks, PetAttackView{
			Name: a.Name, Type: a.Type, Range: attackRange(a), Description: a.Description,
			ToHit:  eval(a.ToHit) + k.sum(TargetAttackAll),
			Damage: damageText(a.DamageDice, eval(a.Damage)+k.sum(TargetDamageAll), a.DamageType),
		})
	}

	for _, m := range mods {
		if m.Kind == ModNote || m.Condition != "" || m.Dice != "" || m.Kind == ModAdvantage || m.Kind == ModDisadvantage {
			v.Notes = append(v.Notes, m)
		}
	}

	for _, src := range uniqueStrings(badTerms) {
		v.Warnings = append(v.Warnings, mkWarning(WarnUnknownTerm, p.ID, "", "", src,
			fmt.Sprintf("%s's stat block has a term %q that can't be worked out (counted as 0)", petLabel(p), src)))
	}
	return v
}

// ownerContext is what a pet's owner.* terms read. The spell terms use the spellcasting of the
// feature or spell that grants the pet.
func ownerContext(c Character, sh Sheet, lib Library, sourceID string) ownerCtx {
	o := ownerCtx{modOf: map[Ability]int{}, pb: sh.ProficiencyBonus, level: sh.Level, classLevels: map[string]int{}}
	for a, av := range sh.Abilities {
		o.modOf[a] = av.Mod
	}
	for _, cl := range c.Classes {
		o.classLevels[cl.ClassID] += cl.Level
	}
	if src, ok := ownerSpellSource(sh, lib, sourceID); ok {
		o.spellAttack, o.spellSaveDC, o.hasSpellcasting = src.AttackBonus, src.SaveDC, true
	}
	return o
}

// ownerSpellSource finds the spellcasting a pet's source uses: a feature's own spell grant, its
// class's spellcasting, or whichever source can cast the granting spell.
func ownerSpellSource(sh Sheet, lib Library, sourceID string) (SpellSourceView, bool) {
	find := func(id string) (SpellSourceView, bool) {
		for _, s := range sh.Spellcasting {
			if s.ID == id {
				return s, true
			}
		}
		return SpellSourceView{}, false
	}
	for _, fv := range sh.Features {
		if fv.FeatureID != sourceID {
			continue
		}
		if f, err := lib.Feature(sourceID); err == nil && f.Spells != nil {
			return find("feature:" + sourceID)
		}
		if fv.Source == OriginClass {
			return find("class:" + fv.SourceID)
		}
		return SpellSourceView{}, false
	}
	for _, s := range sh.Spellcasting {
		for _, sp := range append(append([]SpellView{}, s.Spells...), s.Cantrips...) {
			if sp.ID == sourceID {
				return s, true
			}
		}
	}
	return SpellSourceView{}, false
}

func fillOwnerPlaceholders(note string, o ownerCtx) string {
	if !strings.Contains(note, "{owner.") {
		return note
	}
	return strings.NewReplacer(
		"{owner.spell_save_dc}", fmt.Sprint(o.spellSaveDC),
		"{owner.spell_attack}", fmt.Sprintf("%+d", o.spellAttack),
	).Replace(note)
}

func attackRange(a CreatureAttack) string {
	switch {
	case a.Ranged && a.RangeLong > 0:
		return fmt.Sprintf("%d/%d ft.", a.RangeNormal, a.RangeLong)
	case a.Ranged:
		return fmt.Sprintf("%d ft.", a.RangeNormal)
	}
	reach := a.Reach
	if reach == 0 {
		reach = 5
	}
	return fmt.Sprintf("reach %d ft.", reach)
}

// damageText is "1d8+5 piercing", "1d4-1 piercing" or, with no dice, "1 slashing".
func damageText(dice string, flat int, typ DamageType) string {
	s := fmt.Sprint(flat)
	if dice != "" {
		s = dice
		if flat != 0 {
			s += fmt.Sprintf("%+d", flat)
		}
	}
	if typ != "" {
		s += " " + string(typ)
	}
	return s
}

func mergeDamageTypes(lists ...[]DamageType) []DamageType {
	out := []DamageType{}
	for _, l := range lists {
		for _, d := range l {
			if !slices.Contains(out, d) {
				out = append(out, d)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func uniqueStrings(in []string) []string {
	var out []string
	for _, s := range in {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

func petLabel(p Pet) string {
	if p.Name != "" {
		return p.Name
	}
	return p.CreatureID
}
