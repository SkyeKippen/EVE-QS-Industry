package main

import (
	"QS-Indy/src/db"
	"QS-Indy/src/esi"
	"QS-Indy/src/location"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"text/tabwriter"
)

// Prints where each of a character's (or, with -corp, their corporation's)
// blueprints is. Needs tools/db/generate_locations_table.sql to have been run.
func main() {
	characterFlag := flag.Int64("character", 0, "character ID to use (optional when only one character has signed in)")
	corpFlag := flag.Bool("corp", false, "look up the character's corporation blueprints instead")
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

	owner := location.Owner{CharacterId: character.CharacterID, AccessToken: accessToken}
	var blueprints []esi.Blueprint
	if *corpFlag {
		owner.CorporationId, err = esi.GetCharacterCorporation(int(character.CharacterID))
		if err != nil {
			log.Fatal(err)
		}
		blueprints, err = esi.QueryCorporationBlueprints(owner.CorporationId, accessToken)
	} else {
		blueprints, err = esi.QueryCharacterBlueprints(int(character.CharacterID), accessToken)
	}
	if err != nil {
		log.Fatal(err)
	}

	refs := make([]location.Ref, len(blueprints))
	for i, bp := range blueprints {
		refs[i] = location.Ref{LocationId: bp.LocationId, LocationFlag: bp.LocationFlag}
	}
	locations, err := db.NewLocationResolver().Resolve(ctx, owner, refs)
	if err != nil {
		log.Fatal(err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ItemID\tType\tRegion\tSystem\tLocation\tStructure Type\tOwner\tIn Container\tContainer")
	for i, bp := range blueprints {
		loc := locations[refs[i]]
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%t\t%s\n",
			bp.ItemId, db.TypeName(bp.TypeId), loc.Region, loc.System, loc.Name,
			loc.Type, loc.Owner, loc.InContainer, loc.ContainerName)
	}
	w.Flush()
}
