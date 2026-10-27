// dndserver runs the character API.
//
//	go run ./cmd/dndserver
//	go run ./cmd/dndserver -content content/example,content/homebrew -characters data/characters -addr :8080
//
// Flags
//
//	-addr           address to listen on (default :8080)
//	-content        comma-separated content pack folders, in load order; later packs override earlier ones
//	-characters     folder for character files, one JSON file per character
//	-ruleset        optional ruleset JSON file (inspiration cap, slot tables, currencies, encumbrance...)
//	-cors-origin    allow browser requests from this origin, e.g. http://localhost:5173, or "*" for any
//	-strict-content refuse to start (and to reload) if the content has errors
//	-openapi        print the OpenAPI description of the API to standard output and exit
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	// If your module path differs, change this import to match the "module" line in go.mod.
	character "Users/sylviemckernan/Documents/Projects/DND"
)

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	content := flag.String("content", "content/example", "comma-separated content pack folders, in load order")
	characters := flag.String("characters", "data/characters", "folder for character files")
	rulesetPath := flag.String("ruleset", "", "optional ruleset JSON file")
	cors := flag.String("cors-origin", "", `allowed CORS origin, e.g. http://localhost:5173, or "*" (empty disables CORS)`)
	strict := flag.Bool("strict-content", false, "refuse to start (or reload) if the content has errors")
	openapi := flag.Bool("openapi", false, "print the OpenAPI description of the API and exit")
	flag.Parse()

	if *openapi {
		os.Stdout.Write(character.OpenAPIJSON())
		return
	}

	var dirs []string
	for _, d := range strings.Split(*content, ",") {
		if d = strings.TrimSpace(d); d != "" {
			dirs = append(dirs, d)
		}
	}

	load := func() (*character.ContentLibrary, error) {
		lib, err := character.LoadDirs(character.LoadOptions{Strict: *strict}, dirs...)
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

	store, err := character.NewFileStore(*characters)
	if err != nil {
		log.Fatalf("characters: %v", err)
	}

	srv := character.NewServer(character.Config{
		Store:      store,
		Library:    lib,
		Reload:     load,
		Ruleset:    rules,
		CORSOrigin: *cors,
	})

	httpSrv := &http.Server{Addr: *addr, Handler: srv, ReadHeaderTimeout: 10 * time.Second}
	log.Printf("listening on %s (characters in %s)", *addr, *characters)
	log.Fatal(httpSrv.ListenAndServe())
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
