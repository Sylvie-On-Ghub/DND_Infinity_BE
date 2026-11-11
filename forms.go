package character

// forms.go - forms (Rage, Bladesong): temporary states switched on over the API. While a form is active
// its modifiers count in the sheet's numbers; while it's off they show in Sheet.Notes as
// "while <form> is active" reminders.

import (
	"fmt"
	"slices"
)

// formEntry is a form the character can switch on, with the feature that declares it.
type formEntry struct {
	def        FormDef
	featureID  string
	resourceID string // empty for NoCost forms
}

// formChoiceEntry is a pick offered when a form is switched on, with the feature that declares it.
type formChoiceEntry struct {
	featureID string
	fc        FormChoice
}

// addForms records the form a feature declares and the form choices it offers. Call it once resOf is set.
func (g *gathered) addForms(f Feature) {
	if f.Form != nil {
		e := formEntry{def: *f.Form, featureID: f.ID}
		if !f.Form.NoCost {
			e.resourceID = g.resOf[f.ID]
		}
		g.forms = append(g.forms, e)
	}
	for _, fc := range f.FormChoices {
		g.formChoices = append(g.formChoices, formChoiceEntry{featureID: f.ID, fc: fc})
	}
}

// form finds a form the character has by id.
func (g *gathered) form(id string) (formEntry, bool) {
	for _, e := range g.forms {
		if e.def.ID == id {
			return e, true
		}
	}
	return formEntry{}, false
}

// resolveForms adds the modifiers picked for each active form, then rewrites every form modifier: an
// active form's apply as they are, the rest become "while <form> is active" notes.
func resolveForms(c Character, g *gathered, mods []Modifier) []Modifier {
	active := map[string]bool{}
	for _, af := range c.ActiveForms {
		e, ok := g.form(af.ID)
		if !ok {
			g.warnings = append(g.warnings, mkWarning(WarnFormUnavailable, "", "", "", af.ID,
				fmt.Sprintf("form %q is active but the character has no feature that grants it, so it has no effect", af.ID)))
			continue
		}
		active[af.ID] = true
		mods = append(mods, g.formPickMods(e, af)...)
	}

	warned := map[string]bool{}
	for _, m := range c.Modifiers {
		if _, ok := g.form(m.Form); m.Form != "" && !ok && !warned[m.Form] {
			warned[m.Form] = true
			g.warnings = append(g.warnings, mkWarning(WarnFormUnknown, m.SourceID, "", m.Target, m.Form,
				fmt.Sprintf("modifier on %q is tied to form %q, which the character doesn't have", m.Target, m.Form)))
		}
	}

	out := slices.Clone(mods)
	for i, m := range out {
		if m.Form == "" {
			continue
		}
		if !active[m.Form] {
			name := m.Form
			if e, ok := g.form(m.Form); ok {
				name = e.def.Name
			}
			cond := "while " + name + " is active"
			if m.Condition != "" {
				cond += ", " + m.Condition
			}
			out[i].Condition = cond
		}
		out[i].Form = ""
	}
	return out
}

// formPickMods checks an active form's stored picks against the choices offered for it and returns the
// picked options' modifiers, tagged to the form. Bad picks warn and are left out.
func (g *gathered) formPickMods(e formEntry, af ActiveForm) []Modifier {
	var mods []Modifier
	offered := map[string]bool{}
	for _, ce := range g.formChoices {
		if ce.fc.Form != e.def.ID {
			continue
		}
		ch := ce.fc.Choice
		offered[ch.ID] = true
		picks := af.Picks[ch.ID]
		if len(picks) == 0 {
			if ch.Count > 0 {
				g.warnings = append(g.warnings, mkWarning(WarnFormPickMissing, ce.featureID, ch.ID, "", e.def.ID,
					fmt.Sprintf("%s is active without a pick for %q", e.def.Name, ch.ID)))
			}
			continue
		}
		for n, id := range picks {
			opt, ok := findOption(ch.Options, id)
			if !ok || n >= ch.Count || slices.Contains(picks[:n], id) {
				g.warnings = append(g.warnings, mkWarning(WarnFormPickInvalid, ce.featureID, ch.ID, "", e.def.ID,
					fmt.Sprintf("%s: pick %q for %q isn't an offered option, repeats an earlier pick or is over the count of %d, so it is ignored", e.def.Name, id, ch.ID, ch.Count)))
				continue
			}
			for _, m := range opt.Modifiers {
				m = withSource(m, ce.featureID)
				m.Form = e.def.ID
				mods = append(mods, m)
			}
		}
	}
	for _, id := range sortedKeys(af.Picks) {
		if !offered[id] {
			g.warnings = append(g.warnings, mkWarning(WarnFormPickInvalid, e.featureID, id, "", e.def.ID,
				fmt.Sprintf("%s has a pick for %q, which is no longer offered, so it is ignored", e.def.Name, id)))
		}
	}
	return mods
}

// formViews lists the forms the character can switch on, for Sheet.Forms.
func formViews(c Character, g *gathered, resources []ResourceView) []FormView {
	var out []FormView
	for _, e := range g.forms {
		v := FormView{ID: e.def.ID, Name: e.def.Name, FeatureID: e.featureID, ResourceID: e.resourceID}
		for _, r := range resources {
			if e.resourceID != "" && r.FeatureID == e.resourceID {
				v.UsesLeft = r.Remaining
			}
		}
		for _, af := range c.ActiveForms {
			if af.ID == e.def.ID {
				v.Active, v.Picks = true, af.Picks
			}
		}
		for _, ce := range g.formChoices {
			if ce.fc.Form == e.def.ID {
				v.Choices = append(v.Choices, ce.fc.Choice)
			}
		}
		out = append(out, v)
	}
	return out
}
