package character

// content_query.go - listing and looking up loaded content, for the API's /content routes and for
// anything that needs to enumerate the library (the Library interface only does lookups).

// ContentKinds are the kinds the API exposes, in URL form.
var ContentKinds = []string{"features", "items", "classes", "spells", "spell-lists", "skills", "backgrounds", "races"}

type ContentSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Items only, so a picker can show and filter them without fetching each one.
	Kind   ItemKind `json:"kind,omitempty"`
	Weight float64  `json:"weight,omitempty"`
	Cost   *Price   `json:"cost,omitempty"`
	Tags   []string `json:"tags,omitempty"`
}

// AllFeatures returns every loaded feature, sorted by id. It is what lets choice options list feats.
func (l *ContentLibrary) AllFeatures() []Feature {
	out := make([]Feature, 0, len(l.features))
	for _, id := range sortedKeys(l.features) {
		out = append(out, l.features[id])
	}
	return out
}

// List returns id and name for every entry of a kind, sorted by id (skills keep their file order).
// Items also carry kind, weight, cost and tags.
func (l *ContentLibrary) List(kind string) ([]ContentSummary, error) {
	out := []ContentSummary{}
	switch kind {
	case "features":
		for _, id := range sortedKeys(l.features) {
			out = append(out, ContentSummary{ID: id, Name: l.features[id].Name})
		}
	case "items":
		for _, id := range sortedKeys(l.items) {
			it := l.items[id]
			out = append(out, ContentSummary{ID: id, Name: it.Name, Kind: it.Kind, Weight: it.Weight, Cost: it.Cost, Tags: it.Tags})
		}
	case "classes":
		for _, id := range sortedKeys(l.classes) {
			out = append(out, ContentSummary{ID: id, Name: l.classes[id].Name})
		}
	case "spells":
		for _, id := range sortedKeys(l.spells) {
			out = append(out, ContentSummary{ID: id, Name: l.spells[id].Name})
		}
	case "spell-lists":
		for _, id := range sortedKeys(l.lists) {
			out = append(out, ContentSummary{ID: id, Name: l.lists[id].Name})
		}
	case "backgrounds":
		out = l.listTagged("background")
	case "races":
		out = l.listTagged("race")
	case "skills":
		for _, s := range l.skills {
			out = append(out, ContentSummary{ID: s.ID, Name: s.Name})
		}
	default:
		return nil, notFound("unknown content kind %q (use features, items, classes, spells, spell-lists, skills, backgrounds or races)", kind)
	}
	return out, nil
}

// Lookup returns the full definition of one entry.
func (l *ContentLibrary) Lookup(kind, id string) (any, error) {
	switch kind {
	case "features":
		if v, ok := l.features[id]; ok {
			return v, nil
		}
	case "items":
		if v, ok := l.items[id]; ok {
			return v, nil
		}
	case "classes":
		if v, ok := l.classes[id]; ok {
			return v, nil
		}
	case "spells":
		if v, ok := l.spells[id]; ok {
			return v, nil
		}
	case "spell-lists":
		if v, ok := l.lists[id]; ok {
			return v, nil
		}
	case "backgrounds", "races":
		tag := "background"
		if kind == "races" {
			tag = "race"
		}
		if v, ok := l.features[id]; ok && contains(v.Tags, tag) {
			return v, nil
		}
	case "skills":
		if i, ok := l.skillIdx[id]; ok {
			return l.skills[i], nil
		}
	default:
		return nil, notFound("unknown content kind %q", kind)
	}
	return nil, notFound("%s %q not found", kind, id)
}

// listTagged lists the features carrying a tag, sorted by id. Backgrounds and races are features tagged
// "background" and "race": the tag marks the thing a character chooses, as opposed to its individual traits.
func (l *ContentLibrary) listTagged(tag string) []ContentSummary {
	out := []ContentSummary{}
	for _, id := range sortedKeys(l.features) {
		if f := l.features[id]; contains(f.Tags, tag) {
			out = append(out, ContentSummary{ID: id, Name: f.Name})
		}
	}
	return out
}
