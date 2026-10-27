// contentcheck loads one or more content packs and reports every problem it finds.
//
//	go run ./cmd/contentcheck content/example
//	go run ./cmd/contentcheck content/core content/homebrew
//
// Packs load in the order given; later packs override earlier ones by id.
// Exit status: 0 = no errors (warnings are fine), 1 = content errors, 2 = couldn't load at all.
package main

import (
	"fmt"
	"os"

	// If your module path differs, change this import to match the "module" line in go.mod.
	character "Users/sylviemckernan/Documents/Projects/DND"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: contentcheck <pack dir> [<pack dir> ...]")
		os.Exit(2)
	}

	lib, err := character.LoadDirs(character.LoadOptions{}, os.Args[1:]...)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}

	errs, warns := 0, 0
	for _, is := range lib.Issues() {
		fmt.Println(is)
		if is.Severity == character.SevError {
			errs++
		} else {
			warns++
		}
	}

	c := lib.Counts()
	fmt.Printf("loaded: %d features, %d classes, %d items, %d spells, %d spell lists, %d skills\n",
		c["features"], c["classes"], c["items"], c["spells"], c["spell_lists"], c["skills"])
	fmt.Printf("%d error(s), %d warning(s)\n", errs, warns)
	if errs > 0 {
		os.Exit(1)
	}
}
