// import copies the JSON character files older versions saved into the database, for one owner.
//
//	go run ./cmd/import -owner <discord_username>
//	go run ./cmd/import -owner <discord_username> -dir data/characters
//
// It is safe to run again: an id the owner already has is reported and left untouched. The files
// themselves aren't changed. The database is DND_DB_PATH, or data/dnd.db if that isn't set.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	// If your module path differs, change this import to match the "module" line in go.mod.
	character "Users/sylviemckernan/Documents/Projects/DND"
)

func main() {
	owner := flag.String("owner", "", "Discord username of the account to import into (required)")
	dir := flag.String("dir", "data/characters", "folder of character JSON files")
	flag.Parse()
	if *owner == "" || flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(context.Background(), *owner, *dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, owner, dir string) error {
	path := os.Getenv("DND_DB_PATH")
	if path == "" {
		path = "data/dnd.db"
	}
	db, err := character.OpenDB(ctx, path)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer db.Close()

	u, err := character.UserByUsername(ctx, db, owner)
	if err != nil {
		return fmt.Errorf("%s: %w (add them with cmd/admin first)", owner, err)
	}
	report, err := character.ImportCharacters(ctx, character.NewSQLiteStore(db), u.ID, dir)
	if err != nil {
		return err
	}
	for _, id := range report.Imported {
		fmt.Printf("imported %s\n", id)
	}
	for _, id := range report.Skipped {
		fmt.Printf("skipped  %s (already imported)\n", id)
	}
	for _, f := range report.Failed {
		fmt.Printf("FAILED   %s: %v\n", f.File, f.Err)
	}
	fmt.Printf("%d imported, %d skipped, %d failed\n", len(report.Imported), len(report.Skipped), len(report.Failed))
	if len(report.Failed) > 0 {
		return fmt.Errorf("some files could not be imported")
	}
	return nil
}
