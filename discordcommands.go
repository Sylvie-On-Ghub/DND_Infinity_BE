package character

// discordcommands.go - the bot's slash commands. Only the sender in sender.go uses REST alone; these
// need a gateway connection, but no privileged intents.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"slices"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	allowSignInCommand  = "allow-signin"
	removeSignInCommand = "remove-signin"
	renameSignInCommand = "rename-signin"
)

// DiscordCommands answers the bot's slash commands. Close it to disconnect.
type DiscordCommands struct {
	session  *discordgo.Session
	db       *sql.DB
	adminIDs []string
}

// StartDiscordCommands connects the bot and registers its commands. Only the Discord accounts in
// adminIDs can use them, whatever permissions Discord shows the command with.
func StartDiscordCommands(botToken string, db *sql.DB, adminIDs []string) (*DiscordCommands, error) {
	session, err := discordgo.New("Bot " + botToken)
	if err != nil {
		return nil, fmt.Errorf("create Discord client: %w", err)
	}
	session.Identify.Intents = discordgo.IntentsNone
	d := &DiscordCommands{session: session, db: db, adminIDs: adminIDs}
	session.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		for _, def := range []*discordgo.ApplicationCommand{allowSignInDefinition(), removeSignInDefinition(), renameSignInDefinition()} {
			if _, err := s.ApplicationCommandCreate(r.User.ID, "", def); err != nil {
				log.Printf("register /%s: %v", def.Name, err)
			}
		}
	})
	session.AddHandler(d.onInteraction)
	if err := session.Open(); err != nil {
		return nil, fmt.Errorf("connect to Discord: %w", err)
	}
	return d, nil
}

func (d *DiscordCommands) Close() error {
	return d.session.Close()
}

// adminCommand hides a command from everyone but server administrators. That is only tidiness: the
// command's own function does the real check.
func adminCommand(name, description string, options ...*discordgo.ApplicationCommandOption) *discordgo.ApplicationCommand {
	adminsOnly := int64(0)
	return &discordgo.ApplicationCommand{
		Name:                     name,
		Description:              description,
		DefaultMemberPermissions: &adminsOnly,
		Contexts:                 &[]discordgo.InteractionContextType{discordgo.InteractionContextGuild},
		Options:                  options,
	}
}

func allowSignInDefinition() *discordgo.ApplicationCommand {
	return adminCommand(allowSignInCommand, "Let someone sign in to the app", &discordgo.ApplicationCommandOption{
		Type:        discordgo.ApplicationCommandOptionUser,
		Name:        "user",
		Description: "The person to add",
		Required:    true,
	})
}

func removeSignInDefinition() *discordgo.ApplicationCommand {
	return adminCommand(removeSignInCommand, "Stop someone signing in, and delete their characters",
		&discordgo.ApplicationCommandOption{
			Type:        discordgo.ApplicationCommandOptionUser,
			Name:        "user",
			Description: "The person to remove",
			Required:    true,
		},
		&discordgo.ApplicationCommandOption{
			Type:        discordgo.ApplicationCommandOptionBoolean,
			Name:        "confirm",
			Description: "True permanently deletes their account and characters. Without it, nothing changes.",
		})
}

func renameSignInDefinition() *discordgo.ApplicationCommand {
	return adminCommand(renameSignInCommand, "Update someone's sign-in name to their current Discord username", &discordgo.ApplicationCommandOption{
		Type:        discordgo.ApplicationCommandOptionUser,
		Name:        "user",
		Description: "The person who changed their username",
		Required:    true,
	})
}

func (d *DiscordCommands) onInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}
	data := i.ApplicationCommandData()
	if data.Resolved == nil {
		return
	}
	caller := i.User
	if i.Member != nil {
		caller = i.Member.User
	}
	if caller == nil {
		return
	}
	var target *discordgo.User
	confirm := false
	for _, opt := range data.Options {
		switch opt.Name {
		case "user":
			id, _ := opt.Value.(string)
			target = data.Resolved.Users[id]
		case "confirm":
			confirm, _ = opt.Value.(bool)
		}
	}

	// Discord only waits three seconds for a reply.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var reply string
	switch data.Name {
	case allowSignInCommand:
		reply = allowSignIn(ctx, d.db, d.adminIDs, caller, target)
	case removeSignInCommand:
		reply = removeSignIn(ctx, d.db, d.adminIDs, caller, target, confirm)
	case renameSignInCommand:
		reply = renameSignIn(ctx, d.db, d.adminIDs, caller, target)
	default:
		return
	}
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content:         reply,
			Flags:           discordgo.MessageFlagsEphemeral, // only the caller sees it
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		},
	})
	if err != nil {
		log.Printf("reply to /%s: %v", data.Name, err)
	}
}

// allowSignIn approves target if caller is an admin, and returns the reply to show the caller.
func allowSignIn(ctx context.Context, db *sql.DB, adminIDs []string, caller, target *discordgo.User) string {
	if !slices.Contains(adminIDs, caller.ID) {
		return "You don't have permission to use this command."
	}
	if target == nil {
		return "I couldn't find that user."
	}
	if target.Bot {
		return "Bots can't sign in, because they can't receive login codes."
	}
	u, err := AddUser(ctx, db, target.ID, target.Username)
	switch {
	case errors.Is(err, ErrUserExists):
		return fmt.Sprintf("`%s` couldn't be added, because that account or username is already on the list.", target.Username)
	case err != nil:
		log.Printf("/%s for %s: %v", allowSignInCommand, target.Username, err)
		return "Something went wrong, so nobody was added. Check the server log."
	}
	log.Printf("%s allowed %s to sign in", caller.Username, u.DiscordUsername)
	return fmt.Sprintf("`%s` can now sign in with that username.", u.DiscordUsername)
}

// removeSignIn removes target and their characters if caller is an admin and confirm is set. Without
// confirm it only says what would be deleted. It finds them by Discord id, so it still works after
// they change their username.
func removeSignIn(ctx context.Context, db *sql.DB, adminIDs []string, caller, target *discordgo.User, confirm bool) string {
	if !slices.Contains(adminIDs, caller.ID) {
		return "You don't have permission to use this command."
	}
	if target == nil {
		return "I couldn't find that user."
	}
	failed := func(err error) string {
		log.Printf("/%s for %s: %v", removeSignInCommand, target.Username, err)
		return "Something went wrong, so nobody was removed. Check the server log."
	}
	u, err := userByDiscordID(ctx, db, target.ID)
	if errors.Is(err, ErrUserNotFound) {
		return fmt.Sprintf("`%s` isn't on the list.", target.Username)
	}
	if err != nil {
		return failed(err)
	}
	n, err := CountCharacters(ctx, db, u.ID)
	if err != nil {
		return failed(err)
	}
	if !confirm {
		return fmt.Sprintf("This would permanently delete `%s` and their %d character(s). Nothing was changed. To go ahead, run the command again with `confirm` set to True.", u.DiscordUsername, n)
	}
	if err := RemoveUser(ctx, db, u.ID); err != nil {
		return failed(err)
	}
	log.Printf("%s removed %s and %d character(s)", caller.Username, u.DiscordUsername, n)
	return fmt.Sprintf("Removed `%s` and %d character(s).", u.DiscordUsername, n)
}

// renameSignIn overwrites target's sign-in name with their current Discord username, if caller is an
// admin. Their sessions and characters are unaffected.
func renameSignIn(ctx context.Context, db *sql.DB, adminIDs []string, caller, target *discordgo.User) string {
	if !slices.Contains(adminIDs, caller.ID) {
		return "You don't have permission to use this command."
	}
	if target == nil {
		return "I couldn't find that user."
	}
	failed := func(err error) string {
		log.Printf("/%s for %s: %v", renameSignInCommand, target.Username, err)
		return "Something went wrong, so nothing was changed. Check the server log."
	}
	u, err := userByDiscordID(ctx, db, target.ID)
	if errors.Is(err, ErrUserNotFound) {
		return fmt.Sprintf("`%s` isn't on the list.", target.Username)
	}
	if err != nil {
		return failed(err)
	}
	if u.DiscordUsername == target.Username {
		return fmt.Sprintf("`%s` is already up to date.", u.DiscordUsername)
	}
	err = RenameUser(ctx, db, u.DiscordUsername, target.Username)
	if errors.Is(err, ErrUserExists) {
		return fmt.Sprintf("Couldn't rename `%s`, because another account on the list is already called `%s`.", u.DiscordUsername, target.Username)
	}
	if err != nil {
		return failed(err)
	}
	log.Printf("%s renamed %s to %s", caller.Username, u.DiscordUsername, target.Username)
	return fmt.Sprintf("Renamed `%s` to `%s`. They now sign in with `%s`.", u.DiscordUsername, target.Username, target.Username)
}
