package main

import (
	"QS-Indy/src/db"
	"QS-Indy/src/esi"
	"QS-Indy/src/location"
	"context"
	"flag"
	"log"
)

// Fetches the character's own blueprints and their corporation's, and saves
// both to the blueprints table with their owner.
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

	characterBlueprints, err := esi.QueryCharacterBlueprints(int(character.CharacterID), accessToken)
	if err != nil {
		log.Fatal(err)
	}
	characterOwner := db.BlueprintOwner{Id: character.CharacterID, Type: db.OwnerCharacter, Name: character.CharacterName}
	save(ctx, characterOwner, location.Owner{CharacterId: character.CharacterID, AccessToken: accessToken}, characterBlueprints)

	corporationId, err := esi.GetCharacterCorporation(int(character.CharacterID))
	if err != nil {
		log.Fatal(err)
	}
	names, err := esi.GetNames([]int64{corporationId})
	if err != nil {
		log.Fatal(err)
	}
	corporationBlueprints, err := esi.QueryCorporationBlueprints(corporationId, accessToken)
	if err != nil {
		log.Fatal(err)
	}
	corporationOwner := db.BlueprintOwner{Id: corporationId, Type: db.OwnerCorporation, Name: names[corporationId]}
	save(ctx, corporationOwner, location.Owner{CharacterId: character.CharacterID, CorporationId: corporationId, AccessToken: accessToken}, corporationBlueprints)
}

// save looks up where each blueprint is, then saves them all for owner.
func save(ctx context.Context, owner db.BlueprintOwner, locationOwner location.Owner, blueprints []esi.Blueprint) {
	refs := make([]location.Ref, len(blueprints))
	for i, bp := range blueprints {
		refs[i] = location.Ref{LocationId: bp.LocationId, LocationFlag: bp.LocationFlag}
	}
	locations, err := db.NewLocationResolver().Resolve(ctx, locationOwner, refs)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Attempting to save", len(blueprints), "blueprints for", owner.Name)

	if err := db.SaveBlueprintData(owner, blueprints, locations); err != nil {
		log.Fatal(err)
	}

	log.Println("Successfully saved", len(blueprints), "blueprints for", owner.Name)
}
