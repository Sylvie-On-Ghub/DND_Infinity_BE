package character

import (
	"context"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestAllowSignIn(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	admins := []string{"900"}
	admin := &discordgo.User{ID: "900", Username: "boss"}
	mira := &discordgo.User{ID: "123", Username: "mira"}
	onList := func() bool {
		t.Helper()
		_, err := UserByUsername(ctx, db, "mira")
		return err == nil
	}

	if reply := allowSignIn(ctx, db, admins, &discordgo.User{ID: "901", Username: "mira"}, mira); !strings.Contains(reply, "permission") || onList() {
		t.Errorf("a non-admin, even adding themselves: %q, on list %v", reply, onList())
	}
	if reply := allowSignIn(ctx, db, nil, admin, mira); !strings.Contains(reply, "permission") || onList() {
		t.Errorf("no admins configured: %q, on list %v", reply, onList())
	}
	if reply := allowSignIn(ctx, db, admins, admin, nil); !strings.Contains(reply, "find") {
		t.Errorf("missing target: %q", reply)
	}
	bot := &discordgo.User{ID: "124", Username: "somebot", Bot: true}
	if reply := allowSignIn(ctx, db, admins, admin, bot); !strings.Contains(reply, "Bots") {
		t.Errorf("bot: %q", reply)
	}
	if _, err := UserByUsername(ctx, db, "somebot"); err == nil {
		t.Error("a bot shouldn't be added")
	}

	if reply := allowSignIn(ctx, db, admins, admin, mira); !strings.Contains(reply, "can now sign in") || !onList() {
		t.Errorf("admin: %q, on list %v", reply, onList())
	}
	u, err := UserByUsername(ctx, db, "mira")
	if err != nil || u.DiscordID != "123" {
		t.Errorf("user = %+v (err %v)", u, err)
	}

	if reply := allowSignIn(ctx, db, admins, admin, mira); !strings.Contains(reply, "already") {
		t.Errorf("second time: %q", reply)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	reply := allowSignIn(cancelled, db, admins, admin, &discordgo.User{ID: "125", Username: "noor"})
	if !strings.Contains(reply, "nobody was added") {
		t.Errorf("database failure: %q", reply)
	}
}

func TestAllowSignInDefinitionIsHiddenAndGuildOnly(t *testing.T) {
	def := allowSignInDefinition()
	if def.DefaultMemberPermissions == nil || *def.DefaultMemberPermissions != 0 {
		t.Errorf("default permissions = %v, want administrators only", def.DefaultMemberPermissions)
	}
	if def.Contexts == nil || len(*def.Contexts) != 1 || (*def.Contexts)[0] != discordgo.InteractionContextGuild {
		t.Errorf("contexts = %v", def.Contexts)
	}
	if len(def.Options) != 1 || def.Options[0].Type != discordgo.ApplicationCommandOptionUser || !def.Options[0].Required {
		t.Errorf("options = %+v", def.Options)
	}
}

func TestRemoveSignIn(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	store := NewSQLiteStore(db)
	admins := []string{"900"}
	admin := &discordgo.User{ID: "900", Username: "boss"}
	// Discord now calls them something else: they're matched by id, and shown by the name on the list.
	target := &discordgo.User{ID: "discord-" + testOwner, Username: "renamed"}
	if _, err := store.Create(ctx, testOwner, Character{ID: "a", Name: "A"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(ctx, otherUser, Character{ID: "b", Name: "B"}); err != nil {
		t.Fatal(err)
	}
	stillThere := func() bool {
		t.Helper()
		n, err := CountCharacters(ctx, db, testOwner)
		if err != nil {
			t.Fatal(err)
		}
		_, userErr := userByID(ctx, db, testOwner)
		return n == 1 && userErr == nil
	}

	outsider := &discordgo.User{ID: "901", Username: "outsider"}
	if reply := removeSignIn(ctx, db, admins, outsider, target, true); !strings.Contains(reply, "permission") || !stillThere() {
		t.Errorf("non-admin: %q, still there %v", reply, stillThere())
	}
	if reply := removeSignIn(ctx, db, admins, admin, nil, true); !strings.Contains(reply, "find") {
		t.Errorf("missing target: %q", reply)
	}
	if reply := removeSignIn(ctx, db, admins, admin, &discordgo.User{ID: "555", Username: "stranger"}, true); !strings.Contains(reply, "isn't on the list") {
		t.Errorf("not on the list: %q", reply)
	}
	reply := removeSignIn(ctx, db, admins, admin, target, false)
	if !strings.Contains(reply, "1 character(s)") || !strings.Contains(reply, "Nothing was changed") || !strings.Contains(reply, "name-"+testOwner) || !stillThere() {
		t.Errorf("without confirm: %q, still there %v", reply, stillThere())
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if reply := removeSignIn(cancelled, db, admins, admin, target, true); !strings.Contains(reply, "nobody was removed") || !stillThere() {
		t.Errorf("database failure: %q, still there %v", reply, stillThere())
	}

	if reply := removeSignIn(ctx, db, admins, admin, target, true); !strings.Contains(reply, "Removed") || !strings.Contains(reply, "1 character(s)") {
		t.Errorf("confirmed: %q", reply)
	}
	if _, err := userByID(ctx, db, testOwner); err == nil {
		t.Error("the user should be gone")
	}
	if n, _ := CountCharacters(ctx, db, testOwner); n != 0 {
		t.Errorf("%d characters left", n)
	}
	if n, _ := CountCharacters(ctx, db, otherUser); n != 1 {
		t.Errorf("someone else's characters: %d, want 1", n)
	}
	if reply := removeSignIn(ctx, db, admins, admin, target, true); !strings.Contains(reply, "isn't on the list") {
		t.Errorf("second time: %q", reply)
	}
}

func TestRemoveSignInDefinition(t *testing.T) {
	def := removeSignInDefinition()
	if def.DefaultMemberPermissions == nil || *def.DefaultMemberPermissions != 0 {
		t.Errorf("default permissions = %v", def.DefaultMemberPermissions)
	}
	if len(def.Options) != 2 || !def.Options[0].Required || def.Options[1].Required || def.Options[1].Type != discordgo.ApplicationCommandOptionBoolean {
		t.Errorf("options = %+v", def.Options)
	}
	// Discord refuses longer descriptions.
	if len(def.Description) > 100 || len(def.Options[1].Description) > 100 {
		t.Errorf("descriptions too long: %d, %d", len(def.Description), len(def.Options[1].Description))
	}
}

func TestRenameSignIn(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	store := NewSQLiteStore(db)
	admins := []string{"900"}
	admin := &discordgo.User{ID: "900", Username: "boss"}
	discordID := "discord-" + testOwner
	oldName := "name-" + testOwner
	nameOnList := func() string {
		t.Helper()
		u, err := userByID(ctx, db, testOwner)
		if err != nil {
			t.Fatal(err)
		}
		return u.DiscordUsername
	}
	if _, err := store.Create(ctx, testOwner, Character{ID: "a", Name: "A"}); err != nil {
		t.Fatal(err)
	}

	renamed := &discordgo.User{ID: discordID, Username: "newname"}
	outsider := &discordgo.User{ID: "901", Username: "outsider"}
	if reply := renameSignIn(ctx, db, admins, outsider, renamed); !strings.Contains(reply, "permission") || nameOnList() != oldName {
		t.Errorf("non-admin: %q, name %q", reply, nameOnList())
	}
	if reply := renameSignIn(ctx, db, admins, admin, nil); !strings.Contains(reply, "find") {
		t.Errorf("missing target: %q", reply)
	}
	if reply := renameSignIn(ctx, db, admins, admin, &discordgo.User{ID: "555", Username: "stranger"}); !strings.Contains(reply, "isn't on the list") {
		t.Errorf("not on the list: %q", reply)
	}
	if reply := renameSignIn(ctx, db, admins, admin, &discordgo.User{ID: discordID, Username: oldName}); !strings.Contains(reply, "up to date") {
		t.Errorf("same name: %q", reply)
	}
	taken := &discordgo.User{ID: discordID, Username: "name-" + otherUser}
	if reply := renameSignIn(ctx, db, admins, admin, taken); !strings.Contains(reply, "already called") || nameOnList() != oldName {
		t.Errorf("taken name: %q, name %q", reply, nameOnList())
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if reply := renameSignIn(cancelled, db, admins, admin, renamed); !strings.Contains(reply, "nothing was changed") || nameOnList() != oldName {
		t.Errorf("database failure: %q, name %q", reply, nameOnList())
	}

	if reply := renameSignIn(ctx, db, admins, admin, renamed); !strings.Contains(reply, "Renamed") || nameOnList() != "newname" {
		t.Errorf("rename: %q, name %q", reply, nameOnList())
	}
	if n, _ := CountCharacters(ctx, db, testOwner); n != 1 {
		t.Errorf("characters = %d, want 1", n)
	}
	// A change of case alone counts as a rename.
	if reply := renameSignIn(ctx, db, admins, admin, &discordgo.User{ID: discordID, Username: "NewName"}); !strings.Contains(reply, "Renamed") || nameOnList() != "NewName" {
		t.Errorf("case change: %q, name %q", reply, nameOnList())
	}
}

func TestRenameSignInDefinition(t *testing.T) {
	def := renameSignInDefinition()
	if def.DefaultMemberPermissions == nil || *def.DefaultMemberPermissions != 0 || len(def.Options) != 1 || !def.Options[0].Required {
		t.Errorf("definition = %+v", def)
	}
	if len(def.Description) > 100 {
		t.Errorf("description too long: %d", len(def.Description))
	}
}
