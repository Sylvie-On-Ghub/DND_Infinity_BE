// admin manages the accounts allowed to sign in.
//
//	go run ./cmd/admin add-user <discord_id> <discord_username>
//	go run ./cmd/admin list-users
//	go run ./cmd/admin rename-user <old_username> <new_username>
//	go run ./cmd/admin remove-user <discord_username> --yes
//
// The database is DND_DB_PATH, or data/dnd.db if that isn't set. It is safe to use while the server
// is running.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	// If your module path differs, change this import to match the "module" line in go.mod.
	character "Users/sylviemckernan/Documents/Projects/DND"
)

const usage = `usage:
  admin add-user <discord_id> <discord_username>
  admin list-users
  admin rename-user <old_username> <new_username>
  admin remove-user <discord_username> --yes   (also deletes their characters)`

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", usage)
	}
	cmd, args := args[0], args[1:]
	wantArgs := map[string]int{"add-user": 2, "list-users": 0, "rename-user": 2, "remove-user": 1}
	want, known := wantArgs[cmd]
	yes := false
	if cmd == "remove-user" && len(args) == 2 && (args[1] == "--yes" || args[1] == "-yes") {
		yes = true
		args = args[:1]
	}
	if !known || len(args) != want {
		return fmt.Errorf("%s", usage)
	}

	path := os.Getenv("DND_DB_PATH")
	if path == "" {
		path = "data/dnd.db"
	}
	db, err := character.OpenDB(ctx, path)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer db.Close()

	switch cmd {
	case "add-user":
		u, err := character.AddUser(ctx, db, args[0], args[1])
		if err != nil {
			return err
		}
		fmt.Printf("added %s (Discord id %s)\n", u.DiscordUsername, u.DiscordID)
		return nil
	case "list-users":
		return listUsers(ctx, db)
	case "rename-user":
		if err := character.RenameUser(ctx, db, args[0], args[1]); err != nil {
			return err
		}
		fmt.Printf("renamed %s to %s\n", args[0], args[1])
		return nil
	default:
		return removeUser(ctx, db, args[0], yes)
	}
}

func listUsers(ctx context.Context, db *sql.DB) error {
	users, err := character.ListUsers(ctx, db)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "USERNAME\tDISCORD ID\tCHARACTERS\tADDED")
	for _, u := range users {
		n, err := character.CountCharacters(ctx, db, u.ID)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", u.DiscordUsername, u.DiscordID, n, u.CreatedAt.Format(time.DateOnly))
	}
	return w.Flush()
}

func removeUser(ctx context.Context, db *sql.DB, username string, yes bool) error {
	u, err := character.UserByUsername(ctx, db, username)
	if err != nil {
		return err
	}
	n, err := character.CountCharacters(ctx, db, u.ID)
	if err != nil {
		return err
	}
	if !yes {
		return fmt.Errorf("this permanently deletes %s and their %d character(s); nothing was changed. Add --yes to go ahead", u.DiscordUsername, n)
	}
	if err := character.RemoveUser(ctx, db, u.ID); err != nil {
		return err
	}
	fmt.Printf("removed %s and %d character(s)\n", u.DiscordUsername, n)
	return nil
}
