// dndserver runs the character API.
//
//	DISCORD_BOT_TOKEN=... go run ./cmd/dndserver
//	DND_DEV_MODE=1 go run ./cmd/dndserver -content content/example,content/homebrew
//
// Without -content it loads every pack in content/, looking again on each content reload.
//
// Environment (see .env.example)
//
//	DISCORD_BOT_TOKEN        the bot that sends login codes
//	DND_DISCORD_ADMIN_IDS    comma-separated Discord user ids allowed to use the bot's /allow-signin, /remove-signin and /rename-signin commands
//	DND_DEV_MODE             1 writes login codes to the log instead, when there is no bot token
//	DND_DB_PATH              path of the SQLite database, created if needed (default data/dnd.db)
//	DND_LISTEN_ADDR          address to listen on (default :8080)
//	DND_CORS_ORIGINS         comma-separated browser origins allowed to call the API, or "*" for any
//	DND_TRUSTED_PROXY_HEADER header holding the client address, only if behind a proxy that sets it
//
// Without a bot token or dev mode the server refuses to start.
//
// Flags
//
//	-addr           overrides DND_LISTEN_ADDR
//	-db             overrides DND_DB_PATH
//	-cors-origin    overrides DND_CORS_ORIGINS
//	-content        comma-separated content pack folders, in load order; later packs override earlier ones (default: every pack in content/, ordered by load_order in pack.json, then name)
//	-ruleset        optional ruleset JSON file (inspiration cap, slot tables, currencies, encumbrance...)
//	-strict-content refuse to start (and to reload) if the content has errors
//	-openapi        print the OpenAPI description of the API to standard output and exit
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	// If your module path differs, change this import to match the "module" line in go.mod.
	character "Users/sylviemckernan/Documents/Projects/DND"
)

func main() {
	addr := flag.String("addr", envOr("DND_LISTEN_ADDR", ":8080"), "address to listen on")
	content := flag.String("content", "", "comma-separated content pack folders, in load order (default: every pack in content/)")
	dbPath := flag.String("db", envOr("DND_DB_PATH", "data/dnd.db"), "path of the SQLite database")
	rulesetPath := flag.String("ruleset", "", "optional ruleset JSON file")
	cors := flag.String("cors-origin", os.Getenv("DND_CORS_ORIGINS"), `comma-separated allowed CORS origins, e.g. http://localhost:5173, or "*" (empty disables CORS)`)
	strict := flag.Bool("strict-content", false, "refuse to start (or reload) if the content has errors")
	openapi := flag.Bool("openapi", false, "print the OpenAPI description of the API and exit")
	flag.Parse()

	if *openapi {
		os.Stdout.Write(character.OpenAPIJSON())
		return
	}

	dirs := splitList(*content)

	load := func() (*character.ContentLibrary, error) {
		packs := dirs
		if len(packs) == 0 {
			var err error
			if packs, err = character.FindPacks("content"); err != nil {
				return nil, err
			}
		}
		log.Printf("content packs: %s", strings.Join(packs, ", "))
		lib, err := character.LoadDirs(character.LoadOptions{Strict: *strict}, packs...)
		if lib != nil {
			for _, is := range lib.Issues() {
				log.Println(is)
			}
		}
		return lib, err
	}

	lib, err := load()
	if err != nil {
		log.Fatalf("content: %v", err)
	}
	c := lib.Counts()
	log.Printf("content loaded: %d features, %d classes, %d items, %d spells, %d skills",
		c["features"], c["classes"], c["items"], c["spells"], c["skills"])

	rules, err := loadRuleset(*rulesetPath)
	if err != nil {
		log.Fatalf("ruleset: %v", err)
	}

	db, err := character.OpenDB(context.Background(), *dbPath)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer db.Close()

	devMode := false
	if v := os.Getenv("DND_DEV_MODE"); v != "" {
		if devMode, err = strconv.ParseBool(v); err != nil {
			log.Fatalf("DND_DEV_MODE: %v", err)
		}
	}
	sender, err := character.NewCodeSender(os.Getenv("DISCORD_BOT_TOKEN"), devMode)
	if err != nil {
		log.Fatal(err)
	}
	if _, ok := sender.(character.ConsoleSender); ok {
		log.Print("DEV MODE: sign-in codes will be written to this log")
	}

	// Sign-in doesn't depend on the commands, so a failure here doesn't stop the server.
	if token, admins := os.Getenv("DISCORD_BOT_TOKEN"), splitList(os.Getenv("DND_DISCORD_ADMIN_IDS")); token != "" && len(admins) > 0 {
		commands, err := character.StartDiscordCommands(token, db, admins)
		if err != nil {
			log.Printf("Discord commands are off: %v", err)
		} else {
			defer commands.Close()
			log.Printf("Discord commands /allow-signin, /remove-signin and /rename-signin are on for %d admin(s)", len(admins))
		}
	}

	auth := character.NewAuth(db, sender)
	go auth.RunCleanup(context.Background(), time.Hour)

	srv := character.NewServer(character.Config{
		Store:              character.NewSQLiteStore(db),
		Auth:               auth,
		Library:            lib,
		Reload:             load,
		Ruleset:            rules,
		CORSOrigins:        splitList(*cors),
		TrustedProxyHeader: os.Getenv("DND_TRUSTED_PROXY_HEADER"),
	})

	httpSrv := &http.Server{Addr: *addr, Handler: srv, ReadHeaderTimeout: 10 * time.Second}
	log.Printf("listening on %s (database %s)", *addr, *dbPath)
	log.Fatal(httpSrv.ListenAndServe())
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func splitList(list string) []string {
	var out []string
	for _, v := range strings.Split(list, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func loadRuleset(path string) (character.Ruleset, error) {
	var r character.Ruleset
	if path == "" {
		return r, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return r, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return r, fmt.Errorf("%s: %w", path, err)
	}
	return r, nil
}
