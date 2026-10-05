package main

import (
	"QS-Indy/src/db"
	"QS-Indy/src/esi"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"text/tabwriter"
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

	blueprints, err := esi.QueryCharacterBlueprints(int(character.CharacterID), accessToken)
	if err != nil {
		log.Fatal(err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ItemID\tLocation\tLocationID\tME\tQty\tRuns\tTE\tTypeID")
	for _, bp := range blueprints {
		fmt.Fprintf(w, "%d\t%s\t%d\t%d\t%d\t%d\t%d\t%d\n",
			bp.ItemId, bp.LocationFlag, bp.LocationId,
			bp.MaterialEfficiency, bp.Quantity, bp.Runs,
			bp.TimeEfficiency, bp.TypeId)
	}
	w.Flush()

	corporationId, err := esi.GetCharacterCorporation(int(character.CharacterID))
	if err != nil {
		log.Fatal(err)
	}
	corporationBlueprints, err := esi.QueryCorporationBlueprints(corporationId, accessToken)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Fprintln(w, "ItemID\tLocation\tLocationID\tME\tQty\tRuns\tTE\tTypeID")
	for _, corpBp := range corporationBlueprints {
		fmt.Fprintf(w, "%d\t%s\t%d\t%d\t%d\t%d\t%d\t%d\n",
			corpBp.ItemId, corpBp.LocationFlag, corpBp.LocationId,
			corpBp.MaterialEfficiency, corpBp.Quantity, corpBp.Runs,
			corpBp.TimeEfficiency, corpBp.TypeId)
	}
	w.Flush()
}
