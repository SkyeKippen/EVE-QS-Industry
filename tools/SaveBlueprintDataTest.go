package main

import (
	"QS-Indy/src/db"
	"QS-Indy/src/esi"
	"context"
	"flag"
	"log"
)

// Fetches the character's own blueprints and their corporation's, works out
// where each one is, and saves them, the same refresh the web app runs in
// the background after a sign-in.
func main() {
	characterFlag := flag.Int64("character", 0, "character ID to use (optional when only one character has signed in)")
	flag.Parse()

	cfg, err := esi.LoadConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	character, err := db.ResolveTokenCharacter(ctx, *characterFlag)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Using character %s (%d)", character.CharacterName, character.CharacterID)

	if err := db.RefreshBlueprints(ctx, cfg, character); err != nil {
		log.Fatal(err)
	}
}
