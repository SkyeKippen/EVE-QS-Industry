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

	if err := esi.LoadEnv(); err != nil {
		log.Fatal(err)
	}

	character, err := db.ResolveTokenCharacter(context.Background(), *characterFlag)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Using character %s (%d)", character.CharacterName, character.CharacterID)

	corporationId, err := esi.GetCharacterCorporation(int(character.CharacterID))
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Corporation ID:", corporationId)
}
