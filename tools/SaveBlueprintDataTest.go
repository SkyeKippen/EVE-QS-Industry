package main

import (
	"QS-Indy/src/db"
	"QS-Indy/src/esi"
	"context"
	"flag"
	"log"
)

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

	accessToken, err := db.GetAccessToken(ctx, cfg, character.CharacterID)
	if err != nil {
		log.Fatal(err)
	}

	corporationId, err := esi.GetCharacterCorporation(int(character.CharacterID))
	if err != nil {
		log.Fatal(err)
	}
	corporationBlueprints, err := esi.QueryCorporationBlueprints(corporationId, accessToken)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Attempting to save", len(corporationBlueprints), "blueprints")

	err = db.SaveBlueprintData(corporationBlueprints)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Successfully saved", len(corporationBlueprints), "blueprints")
}
