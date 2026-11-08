package character

// apidocs.go - generates an OpenAPI 3 description of the API.
//
//   - apiRoutes is the one table describing every route: summary, parameters, request and response types.
//   - Schemas for those types are derived from the real Go structs by reflection (json tags, omitempty,
//     maps, slices, nested structs as $ref components), so adding a field to Sheet or Character shows up
//     in the docs without editing anything here.
//   - The server exposes the result as /api/openapi.json and a Swagger UI page at /api/docs, and
//     "dndserver -openapi" prints it for sharing or committing.
//   - A test checks every documented route really exists, so the table can't quietly rot.
//     (A route added to api.go but missing from this table can't be detected automatically.)

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"
)

const apiVersion = "0.2.0"

// ---------------------------------------------------------------------------
// Types used only to describe responses and bodies
// ---------------------------------------------------------------------------

// ActionResponse is what every action route returns: the fresh sheet, plus a field or two for some actions.
type ActionResponse struct {
	Sheet   Sheet  `json:"sheet"`
	EntryID string `json:"entry_id,omitempty"` // inventory add and move: the entry that was created or moved
	PetID   string `json:"pet_id,omitempty"`   // pet add: the pet that was created
	Removed bool   `json:"removed,omitempty"`  // quantity, selections and acknowledgements: whether something was removed
}

type ContentOverview struct {
	Counts   map[string]int `json:"counts"`
	Issues   []Issue        `json:"issues"`
	Errors   int            `json:"errors"`
	Warnings int            `json:"warnings"`
}

type HealthResponse struct {
	Status string `json:"status"`
}

// EntryPatch lists the only fields of an inventory entry that can be patched. Send any subset; null clears.
type EntryPatch struct {
	Equipped        *bool    `json:"equipped,omitempty"`
	Attuned         *bool    `json:"attuned,omitempty"`
	Stashed         *bool    `json:"stashed,omitempty"`
	Proficient      *bool    `json:"proficient,omitempty"`
	Container       *string  `json:"container,omitempty"`
	Notes           *string  `json:"notes,omitempty"`
	AbilityOverride *string  `json:"ability_override,omitempty"`
	Custom          *Item    `json:"custom,omitempty"`
	Tags            []string `json:"tags,omitempty"`
}

// ---------------------------------------------------------------------------
// The route table
// ---------------------------------------------------------------------------

type queryDoc struct {
	Name        string
	Description string
	Required    bool
}

type routeDoc struct {
	ID          string // operationId
	Method      string
	Path        string
	Tag         string
	Summary     string
	Description string
	Query       []queryDoc
	Request     any    // a value of the request body type, or nil
	Example     string // a JSON example of the request body
	Response    any    // a value of the response type, or nil for no body
	OneOf       []any  // instead of Response: values of the types the body can be, when a parameter decides which
	Status      int    // success status; 0 means 200
	Success     string // what success means
	Errors      []int
}

var paramDocs = map[string]string{
	"id":        "A character id (letters, digits, - and _), or a content id on the content routes.",
	"entryId":   "An inventory entry id, as returned when the item was added or in the sheet's inventory.",
	"petId":     "A pet id, as returned when the pet was added or in the sheet's pets.",
	"featureId": "The id of the feature (or item, for charges) whose resource is being changed, as in the sheet's resources.",
	"kind":      "One of features, items, classes, spells, spell-lists, skills, backgrounds, races, creatures.",
}

var errorDocs = map[int]string{
	400: "Bad request: malformed JSON, an unknown field, an invalid id, or a request that can't be carried out. Rules problems are never errors; they come back as warnings on the sheet.",
	404: "Not found: no such character, inventory entry, pet, feature or content.",
	401: "Sign in required: the bearer token is missing, unknown or expired.",
	409: "`already_exists`: you already have a character with that id. `version_conflict`: the character has changed since you read it, so fetch it again and retry.",
	429: "Too many attempts. The `Retry-After` header says how many seconds to wait.",
	500: "Unexpected server error.",
	501: "Not supported by this server's configuration.",
}

var apiRoutes = []routeDoc{
	// ----- Characters
	{ID: "listCharacters", Method: "GET", Path: "/api/characters", Tag: "Characters",
		Summary: "List characters", Description: "Id, name, level and classes for every saved character, sorted by name.",
		Response: []CharacterSummary{}, Success: "The characters."},
	{ID: "createCharacter", Method: "POST", Path: "/api/characters", Tag: "Characters",
		Summary: "Create a character", Description: "Everything is optional. If `id` is omitted the server generates one. Build the rest up with level-ups, selections and the other actions.",
		Request: Character{}, Example: `{"id":"mira","name":"Mira","abilities":{"dex":{"score":16}},"classes":[{"class_id":"rogue","level":1,"rolled_hp":[8]}]}`,
		Response: Character{}, Status: 201, Success: "The saved character. The Location header has its URL.", Errors: []int{400, 409}},
	{ID: "getCharacter", Method: "GET", Path: "/api/characters/{id}", Tag: "Characters",
		Summary: "Get a character", Description: "The stored state: choices and things that change in play. For computed values (AC, bonuses, attacks) use the sheet.",
		Response: Character{}, Success: "The character.", Errors: []int{400, 404}},
	{ID: "replaceCharacter", Method: "PUT", Path: "/api/characters/{id}", Tag: "Characters",
		Summary: "Replace a character", Description: "Replaces the whole character. The character must already exist, and the body's `id` (if given) must match the URL. `version` is required: send the one you read, and if the character has changed since then the request fails with a 409 and nothing is saved.",
		Request: Character{}, Response: Character{}, Success: "The saved character, with its new version.", Errors: []int{400, 404, 409}},
	{ID: "patchCharacter", Method: "PATCH", Path: "/api/characters/{id}", Tag: "Characters",
		Summary:     "Edit some fields of a character",
		Description: "JSON merge patch (RFC 7386): send only what changes. Objects merge, arrays replace whole, and `null` clears a field. The result must still be a valid character, so a misspelt field is a 400. A character's id can't be changed. `version` is required: send the one you read, and if the character has changed since then the request fails with a 409 and nothing is saved.",
		Request:     Character{}, Example: `{"version":3,"inspiration":2,"spellbooks":{"class:wizard":{"prepared":["shield"]}}}`,
		Response: Character{}, Success: "The patched character, with its new version.", Errors: []int{400, 404, 409}},
	{ID: "deleteCharacter", Method: "DELETE", Path: "/api/characters/{id}", Tag: "Characters",
		Summary: "Delete a character", Status: 204, Success: "Deleted.", Errors: []int{400, 404}},

	// ----- Sheet
	{ID: "getSheet", Method: "GET", Path: "/api/characters/{id}/sheet", Tag: "Sheet",
		Summary: "Get the computed sheet", Description: "Everything the rules engine works out for the character: abilities, saves, skills, AC, HP, attacks, spells, inventory and carrying, plus `warnings` and `pending_choices`.",
		Response: Sheet{}, Success: "The sheet.", Errors: []int{400, 404}},
	{ID: "previewSheet", Method: "POST", Path: "/api/sheet-preview", Tag: "Sheet",
		Summary: "Compute a sheet without saving", Description: "Send a full character and get its sheet back. Nothing is stored. Useful for builders that want a live preview while editing.",
		Request: Character{}, Response: Sheet{}, Success: "The sheet.", Errors: []int{400}},

	// ----- Play actions
	{ID: "changeHP", Method: "POST", Path: "/api/characters/{id}/hp", Tag: "Play actions",
		Summary:     "Damage, heal or set hit points",
		Description: "Send exactly one of `damage`, `heal`, `temp` or `set`. Damage hits temporary HP first and stops at 0. Healing stops at max HP (and never lowers HP), and healing from 0 clears death saves. `temp` replaces temp HP. `set` bypasses every rule.",
		Request:     HPAction{}, Example: `{"damage": 7}`, Response: ActionResponse{}, Success: "The updated sheet.", Errors: []int{400, 404}},
	{ID: "changeSpellSlots", Method: "POST", Path: "/api/characters/{id}/spell-slots", Tag: "Play actions",
		Summary:     "Spend or restore spell slots",
		Description: "Relative to what is already used, so two frontends can't overwrite each other. Send `spend` or `restore` with a `level` (1-9), or `pact: true` for pact slots. Overspending is allowed and shows as a sheet warning.",
		Request:     SlotAction{}, Example: `{"level": 3, "spend": 1}`, Response: ActionResponse{}, Success: "The updated sheet.", Errors: []int{400, 404}},
	{ID: "changeResource", Method: "POST", Path: "/api/characters/{id}/resources/{featureId}", Tag: "Play actions",
		Summary:     "Spend or restore a limited-use resource",
		Description: "For uses of a feature, such as Rage or Arcane Recovery. Send exactly one of `spend`, `restore` or `set`.",
		Request:     ResourceAction{}, Example: `{"spend": 1}`, Response: ActionResponse{}, Success: "The updated sheet.", Errors: []int{400, 404}},
	{ID: "rest", Method: "POST", Path: "/api/characters/{id}/rest", Tag: "Play actions",
		Summary:     "Take a short or long rest",
		Description: "Short: pact slots and short-rest resources come back. Long: also HP to max, temp HP cleared, all spell slots back, long-rest and dawn resources back, half your hit dice back (at least 1), death saves cleared, concentration ends, exhaustion drops by 1. Spending hit dice during a short rest is not automated.",
		Request:     restRequest{}, Example: `{"type": "long"}`, Response: ActionResponse{}, Success: "The updated sheet.", Errors: []int{400, 404}},
	{ID: "changeConditions", Method: "POST", Path: "/api/characters/{id}/conditions", Tag: "Play actions",
		Summary:     "Add or remove conditions",
		Description: "All fields optional. Poisoned, frightened and blinded have built-in effects on the sheet; other names are recorded as-is.",
		Request:     ConditionsAction{}, Example: `{"add": ["poisoned"], "remove": ["blinded"], "exhaustion": 1}`, Response: ActionResponse{}, Success: "The updated sheet.", Errors: []int{400, 404}},
	{ID: "levelUp", Method: "POST", Path: "/api/characters/{id}/level-up", Tag: "Play actions",
		Summary:     "Add a level in a class",
		Description: "Adds a level in `class_id` (adding the class at level 1 if the character doesn't have it) and records the hit points for it: `average` (default), `roll`, `max`, or `manual` with a `value`. A character's very first level always gets the maximum. Current HP is not changed. The returned sheet's `pending_choices` say what to ask the player next.",
		Request:     LevelUpRequest{}, Example: `{"class_id":"rogue","hp":{"method":"average"}}`, Response: ActionResponse{}, Success: "The updated sheet.", Errors: []int{400, 404}},

	// ----- Choices
	{ID: "choiceOptions", Method: "GET", Path: "/api/characters/{id}/choice-options", Tag: "Choices",
		Summary:     "List the options for a choice",
		Description: "Each option says whether this character qualifies and, if not, why. The API only reports: a frontend decides whether to hide ineligible options or show them greyed out. Take `feature_id` and `choice_id` from the sheet's `pending_choices`.",
		Query:       []queryDoc{{"feature_id", "The feature holding the choice.", true}, {"choice_id", "The choice's id.", true}},
		Response:    []EligibleOption{}, Success: "The options.", Errors: []int{400, 404}},
	{ID: "setSelection", Method: "POST", Path: "/api/characters/{id}/selections", Tag: "Choices",
		Summary:     "Make or change a choice",
		Description: "Sets or replaces the selection for one feature and choice. Use `picked` for pick choices and `allocated` (option id to points) for allocate choices such as ability score improvements. Out-of-pool or over-count picks are accepted and show as warnings.",
		Request:     Selection{}, Example: `{"feature_id":"rogue_expertise_1","choice_id":"expertise","picked":["stealth","sleight_of_hand"]}`,
		Response: ActionResponse{}, Success: "The updated sheet.", Errors: []int{400, 404}},
	{ID: "removeSelection", Method: "DELETE", Path: "/api/characters/{id}/selections", Tag: "Choices",
		Summary:  "Remove a choice",
		Query:    []queryDoc{{"feature_id", "The feature holding the choice.", true}, {"choice_id", "The choice's id.", true}},
		Response: ActionResponse{}, Success: "The updated sheet; `removed` says whether there was a selection to remove.", Errors: []int{400, 404}},
	{ID: "acknowledgeWarning", Method: "POST", Path: "/api/characters/{id}/acknowledgements", Tag: "Choices",
		Summary:     "Acknowledge a warning",
		Description: "Marks one sheet warning as intentional (\"the DM allowed it\"). Use the warning's `key`. The warning stays on the sheet with `acknowledged: true`; hiding it is up to the frontend. If the offending value changes, the key changes and the acknowledgement lapses.",
		Request:     acknowledgementDoc{}, Example: `{"key":"over_cap:::5","note":"DM approved"}`, Response: ActionResponse{}, Success: "The updated sheet.", Errors: []int{400, 404}},
	{ID: "removeAcknowledgement", Method: "DELETE", Path: "/api/characters/{id}/acknowledgements", Tag: "Choices",
		Summary: "Remove an acknowledgement", Query: []queryDoc{{"key", "The warning key (URL-encode it: keys contain colons).", true}},
		Response: ActionResponse{}, Success: "The updated sheet.", Errors: []int{400, 404}},

	// ----- Pets
	{ID: "addPet", Method: "POST", Path: "/api/characters/{id}/pets", Tag: "Pets",
		Summary:     "Add a pet",
		Description: "`creature_id` is a stat block from /api/content/creatures; `source_id` is the feature or spell that grants it (`find_familiar`, `pact_of_the_chain`...). The pet starts active at full HP. A source the character doesn't have, or one that doesn't allow that creature, is a warning on the pet, not an error. Returns the pet's id in `pet_id`.",
		Request:     AddPetRequest{}, Example: `{"creature_id":"owl","source_id":"find_familiar","name":"Hoot"}`, Response: ActionResponse{}, Success: "The updated sheet and the pet id.", Errors: []int{400, 404}},
	{ID: "patchPet", Method: "PATCH", Path: "/api/characters/{id}/pets/{petId}", Tag: "Pets",
		Summary:     "Change a pet",
		Description: "Send any of `name`, `notes`, `status` (active, dismissed, dead), `creature_id` and `modifiers` (replaces the list). A new `creature_id` is a new form, as when recasting Find Familiar: HP resets to its maximum and temp HP and conditions clear. Setting a dead pet back to active restores its HP.",
		Request:     PetPatch{}, Example: `{"creature_id":"raven"}`, Response: ActionResponse{}, Success: "The updated sheet.", Errors: []int{400, 404}},
	{ID: "petHP", Method: "POST", Path: "/api/characters/{id}/pets/{petId}/hp", Tag: "Pets",
		Summary:     "Damage, heal or set a pet's HP",
		Description: "Same body as the character's HP action. A pet that drops to 0 HP becomes `dead`; healing doesn't revive it.",
		Request:     HPAction{}, Example: `{"damage": 4}`, Response: ActionResponse{}, Success: "The updated sheet.", Errors: []int{400, 404}},
	{ID: "petConditions", Method: "POST", Path: "/api/characters/{id}/pets/{petId}/conditions", Tag: "Pets",
		Summary:     "Add or remove a pet's conditions",
		Description: "Same body as the character's conditions action, without exhaustion.",
		Request:     ConditionsAction{}, Example: `{"add": ["prone"]}`, Response: ActionResponse{}, Success: "The updated sheet.", Errors: []int{400, 404}},
	{ID: "removePet", Method: "DELETE", Path: "/api/characters/{id}/pets/{petId}", Tag: "Pets",
		Summary:  "Remove a pet",
		Response: ActionResponse{}, Success: "The updated sheet.", Errors: []int{404}},

	// ----- Inventory
	{ID: "addItem", Method: "POST", Path: "/api/characters/{id}/inventory", Tag: "Inventory",
		Summary:     "Add an item",
		Description: "Send either `item_id` (from /api/content/items) or an inline `custom` item for one-off loot. Quantity defaults to 1. Weapons, armour and shields can also take `tags`, such as `silvered` or `+1`. A plain item merges into an existing stack of the same item with the same tags in the same container unless `new_stack` is true. Returns the entry's id in `entry_id`.",
		Request:     AddItemRequest{}, Example: `{"item_id":"rope_hempen","quantity":2}`, Response: ActionResponse{}, Success: "The updated sheet and the entry id.", Errors: []int{400, 404}},
	{ID: "patchEntry", Method: "PATCH", Path: "/api/characters/{id}/inventory/{entryId}", Tag: "Inventory",
		Summary:     "Change an entry's state",
		Description: "Equip, attune, stash, move into a container, add notes, or set tags. Only the fields below can be patched. `tags` replaces the entry's whole list and applies to the whole stack; to tag one of several, split it off first with the move action. Equipping two suits of armour is allowed and shows as a warning. On a custom entry, `custom` is a merge patch onto its inline item: send only the item fields that change (`null` clears one). Sending `custom` for a library item is a 400.",
		Request:     EntryPatch{}, Example: `{"equipped": true, "notes": "my pack", "custom": {"weight": 2}}`, Response: ActionResponse{}, Success: "The updated sheet.", Errors: []int{400, 404}},
	{ID: "changeQuantity", Method: "POST", Path: "/api/characters/{id}/inventory/{entryId}/quantity", Tag: "Inventory",
		Summary:     "Change a stack's quantity",
		Description: "Relative. An entry that reaches 0 is removed, and anything stored inside it moves to the top level.",
		Request:     quantityDoc{}, Example: `{"delta": -1}`, Response: ActionResponse{}, Success: "The updated sheet; `removed` says whether the entry is gone.", Errors: []int{400, 404}},
	{ID: "moveEntry", Method: "POST", Path: "/api/characters/{id}/inventory/{entryId}/move", Tag: "Inventory",
		Summary:     "Move an entry into or out of a container",
		Description: "`container` is another entry's id, or empty for the top level. With a `quantity` smaller than the stack, only that many move and a new entry is created for them. Putting something inside itself is a 400.",
		Request:     MoveRequest{}, Example: `{"container": "", "quantity": 1}`, Response: ActionResponse{}, Success: "The updated sheet and the id of the entry that moved.", Errors: []int{400, 404}},
	{ID: "removeEntry", Method: "DELETE", Path: "/api/characters/{id}/inventory/{entryId}", Tag: "Inventory",
		Summary:     "Remove an entry",
		Description: "Anything stored inside it moves to the top level, unless `contents=delete`.",
		Query:       []queryDoc{{"contents", "Set to `delete` to remove the entry's contents too.", false}},
		Response:    ActionResponse{}, Success: "The updated sheet.", Errors: []int{400, 404}},

	// ----- Coins
	{ID: "changeCoins", Method: "POST", Path: "/api/characters/{id}/coins", Tag: "Coins",
		Summary:     "Gain, spend, pay or convert coins",
		Description: "Send exactly one of `gain`, `spend`, `pay` or `convert`. Spending is exact: the named coins leave, and a shortfall leaves a negative balance with a warning rather than failing. `pay` is a price paid by value from whatever coins are held, largest first, with change: `{\"pay\":{\"gp\":15}}` from 10 gp and 100 sp leaves 50 sp. If the coins held are worth less than the price, `pay` fails with 400 code `cant_afford` and nothing changes. `convert` makes change (for example 1 pp to 10 gp) and fails with 400 if the result isn't a whole number of the target coin. Currencies come from /api/ruleset.",
		Request:     CoinsAction{}, Example: `{"gain":{"gp":50}}`, Response: ActionResponse{}, Success: "The updated sheet.", Errors: []int{400, 404}},

	// ----- Content
	{ID: "contentOverview", Method: "GET", Path: "/api/content", Tag: "Content",
		Summary: "Content overview and problems", Description: "How much content is loaded, plus every problem the loader found. Useful for homebrew authors.",
		Response: ContentOverview{}, Success: "Counts and issues."},
	{ID: "reloadContent", Method: "POST", Path: "/api/content/reload", Tag: "Content",
		Summary: "Reload content from disk", Description: "Re-reads the content packs, so homebrew edits show up without restarting. If loading fails, the previous content stays in use.",
		Response: ContentOverview{}, Success: "Counts and issues for the new content.", Errors: []int{500, 501}},
	{ID: "listContent", Method: "GET", Path: "/api/content/{kind}", Tag: "Content",
		Summary:     "List content of one kind",
		Description: "`backgrounds` and `races` list the features tagged that way: the things a character can choose, not their individual traits.",
		Response:    []ContentSummary{}, Success: "Ids and names; items also include kind, weight, cost and tags.", Errors: []int{404}},
	{ID: "getContent", Method: "GET", Path: "/api/content/{kind}/{id}", Tag: "Content",
		Summary: "Get one content definition", Description: "The full definition, typed by `kind`: `classes` returns a Class, `features`, `backgrounds` and `races` a Feature, `items` an Item, `spells` a Spell, `spell-lists` a SpellList and `skills` a SkillDef.",
		OneOf: []any{Class{}, Feature{}, Item{}, Spell{}, SpellList{}, SkillDef{}}, Success: "The definition.", Errors: []int{404}},

	// ----- Account
	{ID: "requestLoginCode", Method: "POST", Path: "/api/auth/otp/request", Tag: "Account",
		Summary:     "Ask for a login code",
		Description: "Sends a 6-digit code by Discord DM, valid for 10 minutes, if the username is approved. The response is the same whether or not it is, and whether or not the DM could be sent. Asking again replaces any earlier code. Limited to 1 request a minute and 5 an hour per username, plus a limit per client address.",
		Request:     otpRequest{}, Example: `{"username":"mira"}`, Response: OTPRequested{}, Status: 202, Success: "Accepted.", Errors: []int{400, 429}},
	{ID: "verifyLoginCode", Method: "POST", Path: "/api/auth/otp/verify", Tag: "Account",
		Summary:     "Sign in with a login code",
		Description: "Exchanges a code for a session token, which is only ever returned here. A code works once, and stops working after 5 failed attempts. An unknown username, a wrong code and an expired code all get the same 401.",
		Request:     otpVerifyRequest{}, Example: `{"username":"mira","code":"123456"}`, Response: SessionResponse{}, Success: "Signed in.", Errors: []int{400, 401, 429}},
	{ID: "logout", Method: "POST", Path: "/api/auth/logout", Tag: "Account",
		Summary: "Sign out", Description: "Ends the session this token belongs to. Other devices stay signed in.", Status: 204, Success: "Signed out."},
	{ID: "getMe", Method: "GET", Path: "/api/me", Tag: "Account",
		Summary: "Who am I", Response: User{}, Success: "The signed-in user."},

	// ----- Server
	{ID: "getRuleset", Method: "GET", Path: "/api/ruleset", Tag: "Server",
		Summary: "Get the ruleset in force", Description: "Currencies, spell slot tables, the inspiration cap and other table-level settings, with defaults filled in.",
		Response: Ruleset{}, Success: "The ruleset."},
	{ID: "health", Method: "GET", Path: "/api/health", Tag: "Server",
		Summary: "Health check", Response: HealthResponse{}, Success: "The server is running."},
}

// Doc-only aliases so the generated schemas get readable names.
type acknowledgementDoc = ackRequest
type quantityDoc = quantityRequest

// ---------------------------------------------------------------------------
// Descriptions for the schemas
// ---------------------------------------------------------------------------

var schemaNames = map[reflect.Type]string{
	reflect.TypeOf(errorBody{}):        "ErrorResponse",
	reflect.TypeOf(errorDetail{}):      "ErrorDetail",
	reflect.TypeOf(restRequest{}):      "RestRequest",
	reflect.TypeOf(ackRequest{}):       "AcknowledgementRequest",
	reflect.TypeOf(quantityRequest{}):  "QuantityRequest",
	reflect.TypeOf(otpRequest{}):       "LoginCodeRequest",
	reflect.TypeOf(otpVerifyRequest{}): "LoginCodeVerifyRequest",
}

var typeDocs = map[string]string{
	"Character": "A character's stored state: choices made and things that change in play. Everything derived (AC, bonuses, attacks) is on the Sheet.",
	"Sheet":     "Everything the rules engine computes for a character. Never stored; recomputed on every request.",
}

var fieldDocs = map[string]string{
	"Sheet.warnings":                 "Rules problems found in the character. The API never rejects these: show them, and let the user acknowledge ones that are intentional.",
	"Sheet.pending_choices":          "Choices the character is entitled to but hasn't made. Drives level-up flows. Fill them in with the selections endpoint.",
	"Sheet.notes":                    "Situational modifiers, advantage and disadvantage, and reminders that don't change a number.",
	"Sheet.attacks":                  "Computed attacks for equipped weapons, feature-granted attacks, and an unarmed strike.",
	"Sheet.spellcasting":             "One entry per spell source: classes, plus features and items that grant spells.",
	"Sheet.spell_slots":              "Shared spell slots. Pact slots are separate.",
	"Sheet.pact_slots":               "Pact magic slots. Absent when the character has none.",
	"Sheet.caster_level":             "Combined caster level for multiclass slot tables. Pact casters don't count.",
	"Sheet.inventory":                "Resolved inventory entries with names, weights, costs and containers.",
	"Sheet.pet_sources":              "The features and castable spells that let the character keep a pet (Find Familiar, Pact of the Chain, Primal Companion), each with the creatures it allows and how many active pets it has. Use one to add a pet.",
	"Sheet.pets":                     "Each pet's computed stat block (scaled by its owner where the creature says so) and play state, with its own warnings.",
	"Sheet.carry":                    "Carried weight against capacity, and the encumbrance state.",
	"Sheet.attunement":               "Attuned items against the limit: the ruleset's max_attuned (default 3) changed by attunement.max modifiers. max is -1 when unlimited.",
	"Sheet.wallet":                   "Coins by denomination and their total value in base units.",
	"Sheet.resources":                "Limited-use feature resources with uses spent.",
	"Sheet.features":                 "Every feature the character has, from classes, subclasses, race, background, feats and choices, in the order they resolved. The race and background themselves are listed too.",
	"Sheet.actions":                  "Actions granted by features and equipped items. Those from a feature carry its feature_id and, if it has limited uses, a resource_id matching resources[].feature_id.",
	"Action.feature_id":              "On the sheet: the feature that granted this action, matching features[].feature_id. Absent for item actions. Ignored in content.",
	"Action.resource_id":             "On the sheet: the granting feature's limited-use resource, matching resources[].feature_id. Usually the same as feature_id; a resource attached to a feature's spells is keyed feature:<id>. With resource_from in content, that feature's resource instead. Absent when there is none. Ignored in content.",
	"Resource.short_rest_regain":     "Uses a Short Rest gives back when recharge is a longer rest, such as one use of Channel Divinity. A Long Rest still restores them all.",
	"ResourceView.short_rest_regain": "Uses a Short Rest gives back when recharge is a longer rest. A Long Rest still restores them all.",
	"Action.resource_from":           "In content: a feature whose resource this action spends, such as a subclass's option for the class's Channel Divinity. Never on the sheet, where it becomes resource_id.",
	"FeatureView.resource_id":        "Its limited-use resource, matching resources[].feature_id. Absent when it has none.",
	"FeatureView.feature_id":         "The instance id, as used by resources, selections and actions. A repeatable feat taken twice has two ids.",
	"FeatureView.source":             "The feature's own origin, or else the class, race or background it was reached through. other when neither says.",
	"FeatureView.source_id":          "The class, race or background it belongs to. A feat with no source of its own gives its own id.",
	"FeatureView.granted_by":         "The feature whose grant or choice added this one, for example the ASI feature a feat was picked from. Absent for top-level features.",
	"FeatureView.level":              "The class level it was gained at. Absent for features that don't come from a class.",
	"Sheet.ac_detail":                "Which AC formula was used, and every alternative with whether it was eligible.",
	"Character.coins":                "Currency id to amount. Negative means owed.",
	"Character.selections":           "Choices made on features. Change them with the selections endpoints.",
	"Character.spellbooks":           "Spell choices per source id, for example class:wizard.",
	"Character.spell_slots_used":     "Spell level to slots spent.",
	"Character.resource_used":        "Feature id to uses spent.",
	"Character.acknowledgements":     "Warnings the user has marked as intentional, matched by key.",
	"Character.modifiers":            "Ad hoc modifiers on this character, applied on top of those from features and items.",
	"Character.classes":              "One entry per class, in the order taken. The first is the character's starting class.",
	"Warning.key":                    "A stable identifier. Use it to acknowledge the warning.",
	"Warning.acknowledged":           "True if the character has an acknowledgement matching this key.",
	"Character.version":              "Goes up by one with every change, including play actions. PUT and PATCH must send the latest one you have.",
	"Sheet.version":                  "The character's version at the time of this sheet. Use it for the next PUT or PATCH.",
	"EligibleOption.eligible":        "False when the character doesn't qualify. Frontends decide whether to hide or grey out the option.",
	"Modifier.requires":              "Limits the modifier to certain equipment. armour: a suit of armour is equipped (a shield doesn't count). no_heavy_armour_or_shield: neither is equipped. no_heavy_armour: no heavy armour is equipped, though a shield is fine. no_armour_or_shield: no armour and no shield is equipped. one_handed_melee_alone: only on the attack of a melee weapon that isn't two-handed, with no other weapon equipped. thrown_weapon: only on attacks made by throwing a weapon. melee_weapon: only on attacks made with a melee weapon, so not an unarmed strike. weapon: only on attacks made with a weapon, melee, ranged or thrown.",
	"Modifier.value_speed":           "Uses this speed as the value, for example a swimming speed equal to your walking speed. Pair it with kind min.",
	"Modifier.value_ability":         "Uses this ability's modifier as the value, with value as the minimum (for example Aura of Protection: Charisma, minimum +1). Only with kind add.",
	"Modifier.dice":                  "Makes this a dice rider (for example 1d6 Sneak Attack), shown on attacks rather than added into numbers.",
	"InventoryItem.custom":           "A one-off item defined inline, used instead of item_id when set.",
	"InventoryItem.tags":             "Weapons, armour and shields: variations of the item. +X adds X to hit and damage on a weapon and X to AC on armour or a shield; silvered adds its rules to a weapon's description; other words are labels. Lowercase; at most one +X.",
	"AddItemRequest.tags":            "Weapons, armour and shields: variations such as silvered or +1. See InventoryItem.tags.",
	"EntryPatch.tags":                "Replaces the entry's tags. See InventoryItem.tags.",
	"InventoryView.tags":             "The entry's tags. name and the numbers elsewhere on the sheet already include them.",
	"InventoryItem.container":        "The id of the entry this one is stored in.",
	"InventoryItem.proficient":       "Weapons: proficient with this one weapon regardless of weapon_profs, for example a Pact of the Blade weapon. Pair it with ability_override cha.",
	"InventoryView.container":        "The id of the entry this one is stored in. Empty at the top level.",
	"InventoryView.contents":         "Containers only: total weight of everything stored inside, including nested containers, whether or not it counts toward carried weight. Absent means 0. Compare with container_info.capacity.",
	"InventoryView.container_info":   "Present when the entry is a container, even an empty one. capacity is the weight it holds (0 or absent = no limit); ignores_contents_weight means its contents add nothing to carried weight (Bag of Holding).",
	"Character.background_id":        "A feature tagged background. List them at /api/content/backgrounds and set this with PATCH.",
	"Character.details":              "Descriptive free text: alignment, appearance, personality and so on. The rules engine never reads it. A PATCH can change one field, e.g. {\"details\":{\"faith\":\"Mielikki\"}}.",
	"Character.portrait_url":         "A link to an image for the character, http or https. The server stores the link only; the browser loads the image. Clear it with null in a PATCH.",
	"CharacterSummary.portrait_url":  "The character's portrait link, if set.",
	"Character.race_id":              "A feature tagged race. List them at /api/content/races and set this with PATCH.",
}

var tagDocs = []struct{ Name, Description string }{
	{"Characters", "Create, read, update and delete characters."},
	{"Sheet", "The computed sheet, and a no-save preview."},
	{"Play actions", "Things that happen at the table: damage, rests, slots, conditions, level-ups. Each returns the fresh sheet."},
	{"Choices", "Pending choices, their options, selections, and acknowledging warnings."},
	{"Pets", "Familiars and companions: their stat blocks are on the sheet under `pets`."},
	{"Inventory", "Items, containers and equipment."},
	{"Coins", "Gaining, spending, paying and converting currency."},
	{"Content", "The loaded classes, features, items, spells and skills, and reloading them."},
	{"Account", "Signing in with a code sent by Discord DM, signing out, and who you are."},
	{"Server", "Ruleset and health."},
}

var apiGuide = strings.Join([]string{
	"A rules engine and storage service for D&D-style characters, built to be driven by any frontend.",
	"",
	"## The core ideas",
	"",
	"- **Character vs sheet.** You store a *character*: the choices made and the things that change in play. The server computes the *sheet* from it (AC, bonuses, attacks, spell DCs, carrying weight). You never calculate rules yourself; fetch the sheet.",
	"- **Everything is permissive.** Rules problems (a skill outside the allowed list, spending a slot you don't have, a prerequisite not met) are never errors. The request succeeds and the sheet carries a `warnings` entry. Show warnings, and let the user acknowledge ones that are intentional (homebrew, DM rulings).",
	"- **Pending choices drive wizards.** `sheet.pending_choices` lists every choice the character is entitled to but hasn't made (starting skills, a subclass, an ability score improvement, spells to pick). For each, ask `choice-options` for what's allowed, then record the pick with `selections`.",
	"- **Feats and ability score improvements.** At a feat level the character makes one choice: a feat. Ability Score Improvement is one of the feats, and taking it opens a second pending choice for the two points. That second choice's `feature_id` looks like `ability_score_improvement@rogue_asi_4:feat`, which names the pick it belongs to, so a feat taken twice has two separate sets of points. Always use a `feature_id` exactly as `pending_choices` gives it.",
	"- **Backgrounds and races.** Both are features marked with a tag (`background`, `race`), chosen by setting `background_id` or `race_id` on the character with a PATCH. List the choices at `/api/content/backgrounds` and `/api/content/races`. A background's own picks, such as its ability score increase and any feat it grants, appear in `pending_choices`. So does choosing a race or a background at all (`feature_id` `character`, `choice_id` `race` or `background`) when the loaded content offers any. Ability score increases come from the background; a race does not change ability scores.",
	"- **Actions return the sheet.** Every play action responds with the updated sheet, so the screen can redraw without a second request.",
	"- **Content comes from packs.** Class, item, spell and feature ids come from `/api/content`. Homebrew packs layer over the core one, so the id lists can differ between servers.",
	"- **Strict bodies.** Unknown fields in a request body are a 400, so a typo can't silently drop data.",
	"",
	"## Signing in",
	"",
	"Accounts are approved by the server's administrator; there is no sign-up.",
	"",
	"1. `POST /api/auth/otp/request` with a Discord username. The user gets a code by DM.",
	"2. `POST /api/auth/otp/verify` with the username and code, and keep the `token` it returns.",
	"3. Send `Authorization: Bearer <token>` on every request that needs it. Sessions last 30 days and extend as they are used.",
	"4. A 401 on a request means the session is over: go back to step 1.",
	"",
	"Your characters are visible only to you. Character ids are per user, and someone else's character looks exactly like one that doesn't exist.",
	"",
	"## Versions",
	"",
	"Every character has a `version`, which goes up with every change. It is on the character and on every sheet, including the sheet an action returns.",
	"",
	"- `PUT` and `PATCH` must send the latest `version` you have. If the character has changed since, they fail with 409 `version_conflict` and nothing is saved: fetch it again, reapply the change and retry.",
	"- Play actions, such as damage or spending a slot, don't send a version, because they apply a change relative to the current state. They still move the version on.",
	"",
	"## Building a character",
	"",
	"1. `POST /api/characters` with a name and ability scores.",
	"2. `POST /api/characters/{id}/level-up` once per level.",
	"3. `GET /api/characters/{id}/sheet` and read `pending_choices`.",
	"4. For each pending choice, `GET .../choice-options` then `POST .../selections`.",
	"5. Pick spells by patching `spellbooks` on the character, for example `{\"spellbooks\": {\"class:wizard\": {\"cantrips\": [\"fire_bolt\"]}}}`.",
	"6. Add gear with `POST .../inventory` and money with `POST .../coins`.",
	"7. `POST .../rest` with `long` to start at full health.",
	"",
	"## Errors",
	"",
	"Every error has the shape `{\"error\": {\"code\": \"...\", \"message\": \"...\"}}` with codes `bad_request`, `invalid_id`, `unauthorized`, `not_found`, `already_exists`, `version_conflict`, `bad_conversion`, `rate_limited`, `method_not_allowed`, `not_supported` and `internal`.",
	"",
	"## Notes",
	"",
	"- Browsers on another origin need that origin listed in the server's `DND_CORS_ORIGINS`.",
	"- Actions on one character are serialised, so concurrent play actions don't overwrite each other.",
}, "\n")

// ---------------------------------------------------------------------------
// Schema generation by reflection
// ---------------------------------------------------------------------------

var (
	timeType = reflect.TypeOf(time.Time{})
	rawType  = reflect.TypeOf(json.RawMessage(nil))
)

func docEnums() map[reflect.Type][]string {
	m := map[reflect.Type][]string{}
	add := func(v any, vals ...string) { m[reflect.TypeOf(v)] = vals }
	add(Ability(""), string(Str), string(Dex), string(Con), string(Int), string(Wis), string(Cha))
	add(ProficiencyLevel(""), string(ProfNone), string(ProfHalf), string(ProfTrained), string(ProfExpert))
	add(ActionType(""), string(ActionAction), string(ActionBonusAction), string(ActionReaction), string(ActionFree), string(ActionOther))
	add(ModKind(""), string(ModAdd), string(ModSet), string(ModMin), string(ModMultiply), string(ModAdvantage), string(ModDisadvantage), string(ModNote))
	add(Origin(""), string(OriginClass), string(OriginRace), string(OriginBackground), string(OriginFeat), string(OriginItem), string(OriginOther))
	add(ChoiceKind(""), string(ChoicePick), string(ChoiceAllocate))
	add(ItemKind(""), string(ItemArmour), string(ItemShield), string(ItemWeapon), string(ItemOther))
	add(RechargeType(""), string(RechargeShortRest), string(RechargeLongRest), string(RechargeDawn), string(RechargeNone))
	add(SpeedType(""), string(SpeedWalk), string(SpeedFly), string(SpeedSwim), string(SpeedClimb), string(SpeedBurrow))
	add(SpellSourceKind(""), string(SourceClass), string(SourceFeature), string(SourceItem))
	add(Severity(""), string(SevError), string(SevWarning))
	return m
}

type schemaGen struct {
	components map[string]any
	enums      map[reflect.Type][]string
}

func newSchemaGen() *schemaGen {
	return &schemaGen{components: map[string]any{}, enums: docEnums()}
}

func schemaRef(name string) map[string]any {
	return map[string]any{"$ref": "#/components/schemas/" + name}
}

func schemaName(t reflect.Type) string {
	if n, ok := schemaNames[t]; ok {
		return n
	}
	return t.Name()
}

func withDescription(p map[string]any, desc string) map[string]any {
	if _, isRef := p["$ref"]; isRef {
		// OpenAPI 3.0 ignores siblings of $ref, so wrap it.
		return map[string]any{"allOf": []any{p}, "description": desc}
	}
	out := map[string]any{}
	for k, v := range p {
		out[k] = v
	}
	out["description"] = desc
	return out
}

func jsonFieldName(f reflect.StructField) (name string, skip bool) {
	tag := f.Tag.Get("json")
	if tag == "-" {
		return "", true
	}
	name = strings.Split(tag, ",")[0]
	if name == "" {
		name = f.Name
	}
	return name, false
}

func (g *schemaGen) schema(t reflect.Type) map[string]any {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch t {
	case timeType:
		return map[string]any{"type": "string", "format": "date-time"}
	case rawType:
		return map[string]any{}
	}
	if vals, ok := g.enums[t]; ok {
		return map[string]any{"type": "string", "enum": vals}
	}

	switch t.Kind() {
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": "array", "items": g.schema(t.Elem())}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": g.schema(t.Elem())}
	case reflect.Struct:
		return g.structSchema(t)
	}
	return map[string]any{} // interfaces and anything unexpected: any value
}

// structSchema registers the struct as a named component and returns a reference to it.
func (g *schemaGen) structSchema(t reflect.Type) map[string]any {
	name := schemaName(t)
	if _, done := g.components[name]; !done {
		g.components[name] = map[string]any{} // placeholder, so recursive types terminate
		props := map[string]any{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" { // unexported
				continue
			}
			jsonName, skip := jsonFieldName(f)
			if skip {
				continue
			}
			p := g.schema(f.Type)
			if d, ok := fieldDocs[name+"."+jsonName]; ok {
				p = withDescription(p, d)
			}
			props[jsonName] = p
		}
		comp := map[string]any{"type": "object", "properties": props}
		if d, ok := typeDocs[name]; ok {
			comp["description"] = d
		}
		g.components[name] = comp
	}
	return schemaRef(name)
}

// ---------------------------------------------------------------------------
// The spec
// ---------------------------------------------------------------------------

func pathParams(path string) []string {
	var out []string
	for _, seg := range strings.Split(path, "/") {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			out = append(out, strings.Trim(seg, "{}"))
		}
	}
	return out
}

// routeNeedsAuth says which documented routes need a session. A test checks this against the server.
func routeNeedsAuth(rt routeDoc) bool {
	switch {
	case strings.HasPrefix(rt.Path, "/api/characters"):
		return true
	}
	switch rt.Path {
	case "/api/sheet-preview", "/api/content/reload", "/api/me", "/api/auth/logout":
		return true
	}
	return false
}

func (g *schemaGen) operation(rt routeDoc) map[string]any {
	op := map[string]any{"operationId": rt.ID, "summary": rt.Summary, "tags": []string{rt.Tag}}
	if routeNeedsAuth(rt) {
		op["security"] = []any{map[string]any{"bearerAuth": []any{}}}
		rt.Errors = append([]int{401}, rt.Errors...)
	}
	if rt.Description != "" {
		op["description"] = rt.Description
	}

	params := []any{}
	for _, name := range pathParams(rt.Path) {
		params = append(params, map[string]any{
			"name": name, "in": "path", "required": true,
			"description": paramDocs[name], "schema": map[string]any{"type": "string"},
		})
	}
	for _, q := range rt.Query {
		params = append(params, map[string]any{
			"name": q.Name, "in": "query", "required": q.Required,
			"description": q.Description, "schema": map[string]any{"type": "string"},
		})
	}
	if len(params) > 0 {
		op["parameters"] = params
	}

	if rt.Request != nil {
		media := map[string]any{"schema": g.schema(reflect.TypeOf(rt.Request))}
		if rt.Example != "" {
			media["example"] = json.RawMessage(rt.Example)
		}
		op["requestBody"] = map[string]any{
			"required": true,
			"content":  map[string]any{"application/json": media},
		}
	}

	status := rt.Status
	if status == 0 {
		status = http.StatusOK
	}
	success := rt.Success
	if success == "" {
		success = "Success."
	}
	ok := map[string]any{"description": success}
	var body map[string]any
	if rt.Response != nil {
		body = g.schema(reflect.TypeOf(rt.Response))
	} else if len(rt.OneOf) > 0 {
		alts := []any{}
		for _, v := range rt.OneOf {
			alts = append(alts, g.schema(reflect.TypeOf(v)))
		}
		body = map[string]any{"oneOf": alts}
	}
	if body != nil {
		ok["content"] = map[string]any{"application/json": map[string]any{"schema": body}}
	}
	responses := map[string]any{fmt.Sprint(status): ok}
	for _, code := range rt.Errors {
		responses[fmt.Sprint(code)] = map[string]any{
			"description": errorDocs[code],
			"content": map[string]any{
				"application/json": map[string]any{"schema": schemaRef("ErrorResponse")},
			},
		}
	}
	op["responses"] = responses
	return op
}

func buildOpenAPI() map[string]any {
	g := newSchemaGen()
	g.schema(reflect.TypeOf(errorBody{})) // make sure ErrorResponse exists even if no route lists an error

	paths := map[string]any{}
	for _, rt := range apiRoutes {
		item, _ := paths[rt.Path].(map[string]any)
		if item == nil {
			item = map[string]any{}
			paths[rt.Path] = item
		}
		item[strings.ToLower(rt.Method)] = g.operation(rt)
	}

	tags := []any{}
	for _, t := range tagDocs {
		tags = append(tags, map[string]any{"name": t.Name, "description": t.Description})
	}

	return map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       "DnD Character API",
			"version":     apiVersion,
			"description": apiGuide,
		},
		"servers": []any{map[string]any{"url": "/", "description": "This server"}},
		"tags":    tags,
		"paths":   paths,
		"components": map[string]any{
			"schemas": g.components,
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]any{
					"type": "http", "scheme": "bearer",
					"description": "The token from `POST /api/auth/otp/verify`.",
				},
			},
		},
	}
}

var (
	specOnce sync.Once
	specJSON []byte
)

// OpenAPIJSON returns the OpenAPI description of the API as indented JSON.
func OpenAPIJSON() []byte {
	specOnce.Do(func() {
		data, err := json.MarshalIndent(buildOpenAPI(), "", "  ")
		if err != nil {
			data = []byte(`{"error":"could not build the OpenAPI description"}`)
		}
		specJSON = append(data, '\n')
	})
	return specJSON
}

const docsPage = `<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <title>DnD Character API</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.ui = SwaggerUIBundle({ url: "/api/openapi.json", dom_id: "#swagger-ui", deepLinking: true });
  </script>
</body>
</html>
`

func (s *Server) serveOpenAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(OpenAPIJSON())
}

func (s *Server) serveDocs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(docsPage))
}
