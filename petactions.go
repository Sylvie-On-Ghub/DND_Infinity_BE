package character

// petactions.go - changing a character's pets: adding and removing them, editing them (including
// recasting Find Familiar into a new form), and tracking their HP and conditions in play.
//
// Max HP always comes from the pet's computed stat block, so actions take the sheet computed before
// the change, like ApplyRest does.

import "slices"

type AddPetRequest struct {
	CreatureID string `json:"creature_id"`
	SourceID   string `json:"source_id"`
	Name       string `json:"name"`
}

// PetPatch lists the pet fields that can be patched; send any subset.
type PetPatch struct {
	Name       *string     `json:"name"`
	Notes      *string     `json:"notes"`
	Status     *PetStatus  `json:"status"`
	CreatureID *string     `json:"creature_id"`
	Modifiers  *[]Modifier `json:"modifiers"`
}

// AddPet adds an active pet at full HP and returns its id. A source that doesn't allow the creature
// only warns on the sheet; a creature that isn't in the library is refused, since it has no max HP.
func AddPet(c *Character, sh Sheet, lib Library, req AddPetRequest) (string, error) {
	if req.CreatureID == "" || req.SourceID == "" {
		return "", badReq("creature_id and source_id are required")
	}
	if _, err := lib.Creature(req.CreatureID); err != nil {
		return "", badReq("unknown creature %q", req.CreatureID)
	}
	p := Pet{ID: NewCharacterID(), CreatureID: req.CreatureID, SourceID: req.SourceID, Name: req.Name, Status: PetActive}
	p.CurrentHP = petMaxHP(p, *c, sh, lib)
	c.Pets = append(c.Pets, p)
	return p.ID, nil
}

// PatchPet edits a pet. A new creature_id is a new form: HP resets to its maximum and temp HP and
// conditions clear. Bringing a dead pet back to active restores it to full HP.
func PatchPet(c *Character, sh Sheet, lib Library, petID string, patch PetPatch) error {
	i, err := petIndex(c, petID)
	if err != nil {
		return err
	}
	p := c.Pets[i]
	if patch.Status != nil && !validPetStatus(*patch.Status) {
		return badReq("status %q must be active, dismissed or dead", *patch.Status)
	}
	if patch.CreatureID != nil && *patch.CreatureID != p.CreatureID {
		if _, err := lib.Creature(*patch.CreatureID); err != nil {
			return badReq("unknown creature %q", *patch.CreatureID)
		}
		p.CreatureID = *patch.CreatureID
		p.CurrentHP, p.TempHP, p.Conditions = petMaxHP(p, *c, sh, lib), 0, nil
	}
	if patch.Name != nil {
		p.Name = *patch.Name
	}
	if patch.Notes != nil {
		p.Notes = *patch.Notes
	}
	if patch.Modifiers != nil {
		p.Modifiers = *patch.Modifiers
	}
	if patch.Status != nil {
		if p.Status == PetDead && *patch.Status == PetActive {
			p.CurrentHP = petMaxHP(p, *c, sh, lib)
		}
		p.Status = *patch.Status
	}
	c.Pets[i] = p
	return nil
}

func RemovePet(c *Character, petID string) error {
	i, err := petIndex(c, petID)
	if err != nil {
		return err
	}
	c.Pets = slices.Delete(c.Pets, i, i+1)
	return nil
}

// ApplyPetHP damages, heals or sets a pet's HP with the character's rules. Dropping to 0 kills it;
// healing doesn't bring it back (set its status to active instead).
func ApplyPetHP(c *Character, sh Sheet, petID string, a HPAction) error {
	i, err := petIndex(c, petID)
	if err != nil {
		return err
	}
	p := &c.Pets[i]
	if p.Status == PetDead && a.Heal != nil {
		return nil
	}
	if err := applyHPTo(&p.CurrentHP, &p.TempHP, petViewMaxHP(sh, petID), a); err != nil {
		return err
	}
	if p.CurrentHP == 0 {
		p.Status = PetDead
	}
	return nil
}

// ApplyPetConditions adds and removes a pet's conditions. Pets don't track exhaustion.
func ApplyPetConditions(c *Character, petID string, a ConditionsAction) error {
	i, err := petIndex(c, petID)
	if err != nil {
		return err
	}
	if a.Exhaustion != nil {
		return badReq("pets don't track exhaustion")
	}
	// Reuse the character rules on a scratch character holding the pet's conditions.
	scratch := Character{Conditions: c.Pets[i].Conditions}
	if err := ApplyConditions(&scratch, a); err != nil {
		return err
	}
	c.Pets[i].Conditions = scratch.Conditions
	return nil
}

// restPets is the long-rest part for pets: every pet that isn't dead returns to full HP.
func restPets(c *Character, sh Sheet) {
	for i := range c.Pets {
		if p := &c.Pets[i]; p.Status != PetDead {
			p.CurrentHP, p.TempHP = petViewMaxHP(sh, p.ID), 0
		}
	}
}

func petIndex(c *Character, petID string) (int, error) {
	for i, p := range c.Pets {
		if p.ID == petID {
			return i, nil
		}
	}
	return -1, notFound("no pet %q", petID)
}

func validPetStatus(s PetStatus) bool {
	return s == PetActive || s == PetDismissed || s == PetDead
}

// petMaxHP computes a pet's maximum HP against the owner's sheet, for a pet the sheet doesn't show yet.
func petMaxHP(p Pet, c Character, sh Sheet, lib Library) int {
	return petView(p, c, sh, lib, lib.Skills()).MaxHP
}

// petViewMaxHP reads a pet's maximum HP from the sheet.
func petViewMaxHP(sh Sheet, petID string) int {
	for _, v := range sh.Pets {
		if v.ID == petID {
			return v.MaxHP
		}
	}
	return 0
}
