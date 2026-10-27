package character

// apidocs_test.go - keeps the generated OpenAPI description honest. Written without being able to
// run them: a failure may be a test mistake as much as a code bug.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func specMap(t *testing.T) map[string]any {
	t.Helper()
	var spec map[string]any
	if err := json.Unmarshal(OpenAPIJSON(), &spec); err != nil {
		t.Fatalf("the OpenAPI output is not valid JSON: %v", err)
	}
	return spec
}

func collectRefs(v any, out *[]string) {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if k == "$ref" {
				if s, ok := val.(string); ok {
					*out = append(*out, s)
				}
				continue
			}
			collectRefs(val, out)
		}
	case []any:
		for _, e := range x {
			collectRefs(e, out)
		}
	}
}

// ---------------------------------------------------------------------------
// The table and the spec built from it
// ---------------------------------------------------------------------------

func TestOpenAPIDescribesEveryRoute(t *testing.T) {
	spec := specMap(t)
	if spec["openapi"] != "3.0.3" {
		t.Errorf("openapi = %v, want 3.0.3", spec["openapi"])
	}
	paths := spec["paths"].(map[string]any)

	tags := map[string]bool{}
	for _, tg := range tagDocs {
		tags[tg.Name] = true
	}
	ids := map[string]bool{}
	seen := map[string]bool{}

	for _, rt := range apiRoutes {
		key := rt.Method + " " + rt.Path
		if seen[key] {
			t.Errorf("%s is listed twice", key)
		}
		seen[key] = true

		item, ok := paths[rt.Path].(map[string]any)
		if !ok {
			t.Errorf("%s: path missing from the spec", key)
			continue
		}
		if _, ok := item[strings.ToLower(rt.Method)]; !ok {
			t.Errorf("%s: method missing from the spec", key)
		}
		if rt.ID == "" || ids[rt.ID] {
			t.Errorf("%s: operation id %q is empty or reused", key, rt.ID)
		}
		ids[rt.ID] = true
		if rt.Summary == "" {
			t.Errorf("%s: no summary", key)
		}
		if !tags[rt.Tag] {
			t.Errorf("%s: tag %q has no description in tagDocs", key, rt.Tag)
		}
		for _, p := range pathParams(rt.Path) {
			if paramDocs[p] == "" {
				t.Errorf("%s: path parameter {%s} has no description in paramDocs", key, p)
			}
		}
		for _, code := range rt.Errors {
			if errorDocs[code] == "" {
				t.Errorf("%s: error status %d has no description in errorDocs", key, code)
			}
		}
	}
}

// Every example in the table must be valid JSON that the real request type accepts. This catches an
// example that has drifted from the struct it documents.
func TestOpenAPIExamplesMatchTheirTypes(t *testing.T) {
	for _, rt := range apiRoutes {
		if rt.Example == "" {
			continue
		}
		if rt.Request == nil {
			t.Errorf("%s %s: has an example but no request type", rt.Method, rt.Path)
			continue
		}
		if !json.Valid([]byte(rt.Example)) {
			t.Errorf("%s %s: example is not valid JSON: %s", rt.Method, rt.Path, rt.Example)
			continue
		}
		target := reflect.New(reflect.TypeOf(rt.Request)).Interface()
		dec := json.NewDecoder(bytes.NewReader([]byte(rt.Example)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(target); err != nil {
			t.Errorf("%s %s: the example doesn't fit %T: %v", rt.Method, rt.Path, rt.Request, err)
		}
	}
}

func TestOpenAPIReferencesResolve(t *testing.T) {
	spec := specMap(t)
	schemas := spec["components"].(map[string]any)["schemas"].(map[string]any)

	var refs []string
	collectRefs(spec, &refs)
	if len(refs) == 0 {
		t.Fatal("expected the spec to use $ref")
	}
	for _, ref := range refs {
		name := strings.TrimPrefix(ref, "#/components/schemas/")
		if name == ref {
			t.Errorf("unexpected reference form %q", ref)
			continue
		}
		if _, ok := schemas[name]; !ok {
			t.Errorf("%q points at a schema that doesn't exist", ref)
		}
	}
	for name, comp := range schemas {
		if props, _ := comp.(map[string]any)["properties"].(map[string]any); len(props) == 0 {
			t.Errorf("schema %s has no properties (a recursion placeholder that never got filled in?)", name)
		}
	}
}

func TestOpenAPISchemasFollowTheTypes(t *testing.T) {
	spec := specMap(t)
	schemas := spec["components"].(map[string]any)["schemas"].(map[string]any)

	sheet, ok := schemas["Sheet"].(map[string]any)
	if !ok {
		t.Fatal("no Sheet schema")
	}
	props := sheet["properties"].(map[string]any)
	for _, f := range []string{"level", "warnings", "pending_choices", "ac_detail", "inventory", "wallet", "carry", "spellcasting"} {
		if _, ok := props[f]; !ok {
			t.Errorf("Sheet is missing %q", f)
		}
	}
	if d, _ := props["warnings"].(map[string]any)["description"].(string); d == "" {
		t.Error("Sheet.warnings should carry its description")
	}
	// A documented field that is a $ref gets wrapped, because OpenAPI 3.0 ignores siblings of $ref.
	ac := props["ac_detail"].(map[string]any)
	if _, wrapped := ac["allOf"]; !wrapped || ac["description"] == nil {
		t.Errorf("ac_detail should be wrapped with its description: %v", ac)
	}

	ability := schemas["SkillView"].(map[string]any)["properties"].(map[string]any)["ability"].(map[string]any)
	enum, _ := ability["enum"].([]any)
	found := false
	for _, v := range enum {
		if v == "str" {
			found = true
		}
	}
	if !found {
		t.Errorf("SkillView.ability should be an enum of the six abilities: %v", ability)
	}

	if _, ok := schemas["ErrorResponse"]; !ok {
		t.Error("no ErrorResponse schema")
	}
	if _, ok := schemas["AcknowledgementRequest"]; !ok {
		t.Error("the acknowledgement request should have a readable schema name")
	}
}

// ---------------------------------------------------------------------------
// The schema generator
// ---------------------------------------------------------------------------

type docChild struct {
	Parent *docSample `json:"parent"`
}

type docSample struct {
	Name   string         `json:"name"`
	Count  *int           `json:"count,omitempty"`
	Tags   []string       `json:"tags"`
	Scores map[string]int `json:"scores"`
	Secret string         `json:"-"`
	When   time.Time      `json:"when"`
	Level  Ability        `json:"level"`
	Child  *docChild      `json:"child"`
	Plain  string
	hidden int
}

func TestSchemaGeneratorBasics(t *testing.T) {
	g := newSchemaGen()
	ref := g.schema(reflect.TypeOf(docSample{}))
	if ref["$ref"] != "#/components/schemas/docSample" {
		t.Fatalf("ref = %v", ref)
	}

	comp, ok := g.components["docSample"].(map[string]any)
	if !ok {
		t.Fatal("no docSample component")
	}
	props := comp["properties"].(map[string]any)

	for _, name := range []string{"name", "count", "tags", "scores", "when", "level", "child", "Plain"} {
		if _, ok := props[name]; !ok {
			t.Errorf("missing property %q", name)
		}
	}
	for _, name := range []string{"Secret", "hidden", "-"} {
		if _, ok := props[name]; ok {
			t.Errorf("property %q should be left out", name)
		}
	}

	if props["count"].(map[string]any)["type"] != "integer" {
		t.Errorf("a pointer to int should be an integer: %v", props["count"])
	}
	if props["tags"].(map[string]any)["items"].(map[string]any)["type"] != "string" {
		t.Errorf("tags should be an array of strings: %v", props["tags"])
	}
	if props["scores"].(map[string]any)["additionalProperties"].(map[string]any)["type"] != "integer" {
		t.Errorf("scores should be a map to integers: %v", props["scores"])
	}
	if props["when"].(map[string]any)["format"] != "date-time" {
		t.Errorf("time.Time should be a date-time string: %v", props["when"])
	}
	enum, _ := props["level"].(map[string]any)["enum"].([]string)
	if len(enum) != 6 || enum[0] != "str" {
		t.Errorf("an Ability should be an enum of the six abilities: %v", props["level"])
	}

	// docChild points back at docSample: the recursion must terminate with both components present.
	if _, ok := g.components["docChild"]; !ok {
		t.Error("the nested struct should become its own component")
	}
	if props["child"].(map[string]any)["$ref"] != "#/components/schemas/docChild" {
		t.Errorf("child should be a reference: %v", props["child"])
	}
}

// ---------------------------------------------------------------------------
// Drift: documented routes must exist
// ---------------------------------------------------------------------------

func TestDocumentedRoutesAreReal(t *testing.T) {
	store := NewMemoryStore()
	srv := NewServer(Config{
		Store:  store,
		Reload: func() (*ContentLibrary, error) { return newContentLibrary(), nil },
	})
	subst := strings.NewReplacer("{id}", "doc", "{entryId}", "e1", "{featureId}", "arcane_recovery", "{kind}", "features")

	for _, rt := range apiRoutes {
		saveFixture(t, store, Character{ID: "doc", Name: "Doc"})
		path := subst.Replace(rt.Path)

		var body any
		switch rt.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch:
			body = "{}"
		}
		rec := call(t, srv, rt.Method, path, body)

		var e errorBody
		_ = json.Unmarshal(rec.Body.Bytes(), &e) // not every response is an error; ignore the failure
		if rec.Code == http.StatusMethodNotAllowed || e.Error.Code == "method_not_allowed" {
			t.Errorf("%s %s is documented but the server says the method isn't allowed", rt.Method, rt.Path)
		}
		if e.Error.Message == "no such route" {
			t.Errorf("%s %s is documented but the server has no such route", rt.Method, rt.Path)
		}
	}
}

// ---------------------------------------------------------------------------
// The endpoints that serve the docs
// ---------------------------------------------------------------------------

func TestDocsEndpoints(t *testing.T) {
	srv := NewServer(Config{Store: NewMemoryStore()})

	rec := call(t, srv, http.MethodGet, "/api/openapi.json", nil)
	wantStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Errorf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
	var spec map[string]any
	into(t, rec, &spec)
	if spec["openapi"] != "3.0.3" {
		t.Errorf("openapi = %v", spec["openapi"])
	}

	rec = call(t, srv, http.MethodGet, "/api/docs", nil)
	wantStatus(t, rec, http.StatusOK)
	page := rec.Body.String()
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/html") ||
		!strings.Contains(page, "swagger-ui") || !strings.Contains(page, "/api/openapi.json") {
		t.Errorf("the docs page should be HTML that loads Swagger UI against /api/openapi.json:\n%s", page)
	}

	wantError(t, call(t, srv, http.MethodPost, "/api/docs", nil), http.StatusMethodNotAllowed, "method_not_allowed")
	wantError(t, call(t, srv, http.MethodPost, "/api/openapi.json", nil), http.StatusMethodNotAllowed, "method_not_allowed")
}

func TestOpenAPITypesContentAndFeatures(t *testing.T) {
	spec := specMap(t)
	op := spec["paths"].(map[string]any)["/api/content/{kind}/{id}"].(map[string]any)["get"].(map[string]any)
	schema := op["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	var refs []string
	collectRefs(schema["oneOf"], &refs)
	for _, want := range []string{"Class", "Feature", "Item", "Spell", "SpellList", "SkillDef"} {
		if !contains(refs, "#/components/schemas/"+want) {
			t.Errorf("getContent should offer %s, got %v", want, refs)
		}
	}

	schemas := spec["components"].(map[string]any)["schemas"].(map[string]any)
	props := func(name string) map[string]any {
		s, _ := schemas[name].(map[string]any)
		p, _ := s["properties"].(map[string]any)
		return p
	}
	if _, ok := props("Sheet")["features"]; !ok {
		t.Error("Sheet should document features")
	}
	for _, f := range []string{"feature_id", "name", "description", "source", "source_id", "granted_by", "level", "resource_id"} {
		if _, ok := props("FeatureView")[f]; !ok {
			t.Errorf("FeatureView should document %s", f)
		}
	}
	for _, f := range []string{"feature_id", "resource_id"} {
		if _, ok := props("Action")[f]; !ok {
			t.Errorf("Action should document %s", f)
		}
	}
}
