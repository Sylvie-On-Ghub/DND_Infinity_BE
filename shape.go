package character

// shape.go - shape forms (Wild Shape): the Known Forms choice, the limits a class level sets and which
// known forms the character can take now.

import (
	"fmt"
	"slices"
)

// shapeLimit is the highest limit at or below the class level.
func shapeLimit(def ShapeDef, classLevel int) (ShapeLimit, bool) {
	var best ShapeLimit
	found := false
	for _, l := range def.Limits {
		if l.MinLevel <= classLevel && (!found || l.MinLevel > best.MinLevel) {
			best, found = l, true
		}
	}
	return best, found
}

// knownFormsChoice is the pick of creatures a shape form can become, sized by the class level. It is false
// below the first limit's level.
func knownFormsChoice(def ShapeDef, classLevel int) (Choice, bool) {
	limit, ok := shapeLimit(def, classLevel)
	if !ok {
		return Choice{}, false
	}
	return Choice{ID: KnownFormsChoice, Prompt: "Choose your Known Forms", Kind: ChoicePick, Count: limit.Known,
		Filter: &PoolFilter{Type: "creature", Tag: def.CreatureTag}}, true
}

// shapeFlyFrom is the lowest class level at which a shape allows forms with a fly speed, or 0 for never.
func shapeFlyFrom(def ShapeDef) int {
	from := 0
	for _, l := range def.Limits {
		if l.Fly && (from == 0 || l.MinLevel < from) {
			from = l.MinLevel
		}
	}
	return from
}

// shapeInfo is what a shape form allows the character right now.
type shapeInfo struct {
	entry   formEntry
	limit   ShapeLimit
	maxCR   float64
	flyFrom int // class level from which fly speeds are allowed; 0 for never
	tempHP  int
	known   []string // picked Known Forms, in pick order
}

// shapeInfo reads a shape form's limits at the character's class level. Modifiers on shape.max_cr and
// shape.temp_hp raise them: those tied to this form (or none) and with no condition count.
func (g *gathered) shapeInfo(c Character, e formEntry) (shapeInfo, bool) {
	def := e.def.Shape
	if def == nil {
		return shapeInfo{}, false
	}
	limit, ok := shapeLimit(*def, g.classLevels[def.ClassID])
	if !ok {
		return shapeInfo{}, false
	}
	// The temp HP formula rides through the same resolution as modifier values.
	mods := []Modifier{{Target: TargetShapeTempHP, Kind: ModSet, ValueFormula: &def.TempHP}}
	for _, m := range g.mods {
		if (m.Form == "" || m.Form == e.def.ID) && m.Condition == "" &&
			(m.Target == TargetShapeMaxCR || m.Target == TargetShapeTempHP) {
			mods = append(mods, m)
		}
	}
	mods = resolveFormulaValues(c, g, resolveAbilityValues(c, mods))

	info := shapeInfo{entry: e, limit: limit, flyFrom: shapeFlyFrom(*def), tempHP: mods[0].Value}
	info.maxCR, _ = parseCR(limit.MaxCR)
	for _, m := range mods[1:] {
		switch {
		case m.Target == TargetShapeMaxCR && m.Kind == ModMin:
			info.maxCR = max(info.maxCR, float64(m.Value))
		case m.Target == TargetShapeTempHP && m.Kind == ModSet:
			info.tempHP = max(info.tempHP, m.Value)
		}
	}
	for _, s := range c.Selections {
		if s.FeatureID == e.featureID && s.ChoiceID == KnownFormsChoice {
			info.known = s.Picked
			break
		}
	}
	return info, true
}

// shapeEligible says whether the character can take a creature as this shape now, and why not.
func shapeEligible(info shapeInfo, cr Creature, formName string) (bool, string) {
	if v, _ := parseCR(cr.CR); v > info.maxCR {
		return false, fmt.Sprintf("CR %s is above your max of %s", cr.CR, formatCR(info.maxCR))
	}
	if cr.Speeds[SpeedFly] > 0 && !info.limit.Fly {
		return false, fmt.Sprintf("has a fly speed, which %s allows from level %d", formName, info.flyFrom)
	}
	return true, ""
}

// shapeFormView fills a shape form's limits and known forms. Known ids missing from the library, or
// without the shape's tag, are left out.
func (g *gathered) shapeFormView(c Character, v *FormView, e formEntry) {
	info, ok := g.shapeInfo(c, e)
	if !ok {
		return
	}
	v.MaxCR, v.FlyAllowed, v.TempHP = formatCR(info.maxCR), info.limit.Fly, info.tempHP
	for _, id := range info.known {
		cr, err := g.lib.Creature(id)
		if err != nil || !slices.Contains(cr.Tags, e.def.Shape.CreatureTag) {
			continue
		}
		eligible, why := shapeEligible(info, cr, e.def.Name)
		v.KnownForms = append(v.KnownForms, ShapeOption{ID: cr.ID, Name: cr.Name, CR: cr.CR, Eligible: eligible, Reason: why})
	}
}

// shapeLimitsFor rebuilds a feature's shape info from the sheet's form view, for judging a pick before
// it is made. The form's def is returned too, for its name.
func shapeLimitsFor(sh Sheet, lib Library, featureID string) (shapeInfo, FormDef) {
	f, err := lib.Feature(baseFeatureID(featureID))
	if err != nil || f.Form == nil || f.Form.Shape == nil {
		return shapeInfo{}, FormDef{}
	}
	info := shapeInfo{flyFrom: shapeFlyFrom(*f.Form.Shape)}
	for _, fv := range sh.Forms {
		if fv.ID == f.Form.ID {
			info.maxCR, _ = parseCR(fv.MaxCR)
			info.limit.Fly = fv.FlyAllowed
		}
	}
	return info, *f.Form
}
