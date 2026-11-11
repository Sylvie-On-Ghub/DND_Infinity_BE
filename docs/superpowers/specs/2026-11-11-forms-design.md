# Forms design

Temporary states a character switches on and off during play (Rage, Bladesong, Innate Sorcery,
Dragon Wings, Holy Nimbus). While a form is active its modifiers count in the sheet's numbers;
while it is off they stay situational notes, as they are today. Everything is driven from the API.

## Goals

- Content declares forms; adding one is a JSON edit, not Go code.
- Starting a form spends a use of its resource and refuses when none are left.
- Other features attach riders to a form by naming it (Primal Knowledge, Bladework, Sorcery Incarnate).
- Some forms take picks at activation (Wild Heart's Bear/Eagle/Wolf); picks are stored with the active form.
- The sheet with every form off matches today's sheet apart from condition wording.

## Non-goals

- Rules enforcement: no warnings for concentrating or casting while raging, heavy armour while raging,
  armour during Bladesong, and no auto-ending on Incapacitated. These can be added later as warnings.
- Durations or turn tracking. A form lasts until ended or a long rest.
- Refunding a use when a form ends.
- Discord commands. Forms are API-only.

## Current state

Rage, Bladesong, Dragon Wings and Holy Nimbus modifiers carry free-text `condition` strings
("while raging", "while your Bladesong is active"), and Innate Sorcery's effects are `note` modifiers.
`compute.go` never applies a conditional modifier to a number; it surfaces it in `Sheet.Notes`.
The Wild Heart per-rage options exist only as description text.

## Data model

### Library

```go
// Feature gains:
Form        *FormDef     `json:"form,omitempty"`         // this feature can be switched on as a form
FormChoices []FormChoice `json:"form_choices,omitempty"` // picks made each time a form starts

type FormDef struct {
    ID   string `json:"id"`   // "rage"; unique across the library
    Name string `json:"name"` // "Rage"; the off-state label is "while <Name> is active"
    // Starting the form spends one use of this feature's resource, found through resOf, so a resource
    // on the feature's active tier (Rage) works. NoCost marks a form that spends nothing.
    NoCost bool `json:"no_cost,omitempty"`
}

type FormChoice struct {
    Form   string `json:"form"` // the form this pick belongs to
    Choice        // the existing Choice; only kind "pick" with explicit Options is allowed
}

// Modifier gains:
Form string `json:"form,omitempty"` // applies only while this form is active; Condition still qualifies it
```

Choice options already carry modifiers. Those modifiers are tagged with the form when resolved, so
content does not repeat `form` on them.

New requirement constants in `character.go`:

- `RequiresStrengthAttack = "strength_attack"`: an attack-by-attack requirement. It is met by an attack
  whose ability is Strength (weapon, thrown or unarmed). It sits with `RequiresMeleeWeapon`.
- `RequiresOnlyCaster = "only_caster:<class id>"`: met when the named class is the character's only
  spellcasting class. It is checked in `activeMods`. When it isn't met, the modifier is not dropped. It is
  kept as situational with the condition "with <Class> spells".

### Loader

- Two features declaring the same form id is a load error.
- A modifier or form choice naming a form no feature declares is a load error. Content is checked when it
  loads, so this never reaches a character.
- A form choice that isn't kind `pick` with explicit options is a load error.

### Character (stored play state)

```go
ActiveForms []ActiveForm `json:"active_forms,omitempty"`

type ActiveForm struct {
    ID    string              `json:"id"`              // form id
    Picks map[string][]string `json:"picks,omitempty"` // form choice id -> option ids
}
```

### Sheet (computed)

```go
Forms []FormView `json:"forms,omitempty"`

type FormView struct {
    ID         string              `json:"id"`
    Name       string              `json:"name"`
    FeatureID  string              `json:"feature_id"`            // instance id of the declaring feature
    Active     bool                `json:"active"`
    ResourceID string              `json:"resource_id,omitempty"` // empty for NoCost forms
    UsesLeft   int                 `json:"uses_left,omitempty"`
    Picks      map[string][]string `json:"picks,omitempty"`       // current picks when active
    Choices    []Choice            `json:"choices,omitempty"`     // picks offered at activation
}
```

A form appears only when the character has the declaring feature. A form choice appears only when the
character has the feature declaring it (Rage shows the Bear/Eagle/Wolf pick only for a Wild Heart).

## Compute

1. **Gather.** A feature with `Form` is recorded in `g.forms` with its feature id and its resource id
   from `resOf`. A feature with `FormChoices` records them against their form. For each active form, its
   stored picks are resolved into the chosen options' modifiers, and each is given `Form` = that form.
2. **resolveForms(mods, active)** runs before `activeMods`. For each modifier with `Form` set:
   - form active: clear `Form`, keep `Condition`. With no condition left, the modifier applies.
   - form off: clear `Form`, prefix the condition with "while <Name> is active" (joined with ", " to any
     existing condition). It shows in `Sheet.Notes` as today.
3. **activeMods, dedupe, derive** are unchanged apart from the two new requirements.
4. **Sheet.Forms** is built from `g.forms` and `c.ActiveForms`.

Warnings (permissive, for stale stored state):

- `form_unavailable`: an active form whose declaring feature the character no longer has. Ignored for numbers.
- `form_pick_missing`: an active form lacks a pick its form choices require.
- `form_pick_invalid`: a stored pick names an option that doesn't exist, or exceeds the choice's count.
  The bad option is ignored.
- `form_unknown`: an ad hoc character modifier (`Character.Modifiers`) names a form the character doesn't
  have. The loader can't check these. The modifier is treated as situational with the form id as its condition.

Pets need no change: owner-context formulas read the owner's computed values.

## API

`POST /api/characters/{id}/forms`. It is mutated through the same path as `conditions` and
`resources`, with the pre-action sheet.

```go
type FormAction struct {
    Start *FormStart `json:"start"` // exactly one of start / end
    End   string     `json:"end"`   // form id
}
type FormStart struct {
    ID    string              `json:"id"`
    Picks map[string][]string `json:"picks,omitempty"`
    Free  bool                `json:"free,omitempty"` // don't spend a use
}
func ApplyForm(c *Character, sh Sheet, a FormAction) error
```

- Neither or both of start and end: 400.
- Start, form not in `sh.Forms`: 400. Already active: 400.
- Start, picks: every offered choice must be filled with options from its pool, up to its count. A choice
  offered but not sent, an unknown choice id, or an unknown option: 400.
- Start, cost: unless `Free` or `NoCost`, one use is spent through the same bookkeeping as
  `ApplyResource`. With no uses left: 409 `no_uses`.
- End: removes the form. Ending an inactive form is a no-op. Nothing is refunded.
- Changing picks means ending, then starting again with `free: true`.
- Long rest clears `ActiveForms`. Short rest leaves them.
- `active_forms` can also be written through PUT and PATCH like other play state, subject to the warnings.

`openapi.json` and `apidocs.go` document the route, `active_forms` and `Sheet.Forms`.

## Content changes

**Barbarian Rage** (`barbarian_rage`): `form: {id: "rage", name: "Rage"}`. The resistance and Strength
advantage modifiers change `condition: "while raging"` to `form: "rage"`. Each damage tier becomes
`form: "rage", requires: "strength_attack"` with no condition.

**Rage riders:**
- Primal Knowledge and Relentless Rage notes get `form: "rage"`, and "while raging" is removed from the note text.
- Rage of the Wilds becomes a form choice on `rage`:
  - Bear: resistance to every damage type except Force, Necrotic, Psychic and Radiant, as `resist.*` modifiers.
  - Eagle and Wolf: notes.
- Power of the Wilds becomes a second form choice on `rage`:
  - Falcon: `speed.fly` min equal to walk speed, `requires: "no_armour_or_shield"`. That's the closest
    existing requirement; the rule says no armour, so this is noted as a known approximation.
  - Lion and Ram: notes.
- These four are the only features in the content that reference Rage's state. Instinctive Pounce and
  Persistent Rage describe activation or duration, not effects while raging, and are unchanged.

**Bladesong** (`bladesinger_bladesong`): `form: {id: "bladesong", name: "Bladesong"}`. Its AC, speed,
Acrobatics and Concentration modifiers move from the condition text to `form: "bladesong"`, and the
Concentration one keeps `condition: "to keep Concentration"`. The Bladework note gets `form: "bladesong"`.

**Innate Sorcery** (`sorcerer_innate_sorcery`): `form: {id: "innate_sorcery", name: "Innate Sorcery"}`.
The two notes become real modifiers: `spell.save_dc` add 1 and `spell.attack` advantage, each with
`form: "innate_sorcery", requires: "only_caster:sorcerer"`. The Sorcery Incarnate and Arcane Apotheosis
notes get `form: "innate_sorcery"`.

**Dragon Wings**: `form: {id: "dragon_wings", name: "Dragon Wings"}`. The fly speed modifier moves from
the condition to `form: "dragon_wings"`.

**Holy Nimbus** (legacy Paladin): `form: {id: "holy_nimbus", name: "Holy Nimbus"}`. The save advantage
gets `form: "holy_nimbus"` and keeps `condition: "against spells cast by fiends or undead"`.

## Testing

- `forms_test.go`: on/off relabelling, a surviving residual condition, picks resolving to modifiers,
  `form_unavailable` / `form_pick_missing` / `form_pick_invalid`, long rest clearing forms, and in
  `ApplyForm` spending, 409 when no uses are left, `free`, double start, and end no-op.
- `barbarian_test.go`: Rage on gives B/P/S resistance and Strength save/check advantage, the damage bonus
  on a Strength weapon but not a Dex one, and Bear's resistances. Rage off keeps today's notes.
- `wizard_test.go`: Bladesong on gives AC + Int mod (min 1) and +10 walk speed.
- `sorcerer_test.go`: Innate Sorcery on gives DC +1 and spell attack advantage for a single-class
  sorcerer, but stays situational for a sorcerer/wizard. Dragon Wings on gives fly 60.
- `paladin_test.go`: Holy Nimbus on keeps the save advantage situational.
- `loader_test.go`: duplicate form id, unknown form on a modifier, a non-pick form choice.
- `api_test.go`: the route, 409 `no_uses`, 400 on a bad pick, and `Sheet.Forms` in the sheet response.
