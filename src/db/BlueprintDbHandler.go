package db

import (
	"QS-Indy/src/esi"
	"QS-Indy/src/location"
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/jackc/pgx/v5"
)

func connectDB() (*pgx.Conn, error) {
	conn, err := pgx.Connect(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// Blueprint owner types, stored in blueprints.owner_type.
const (
	OwnerCharacter   = "character"
	OwnerCorporation = "corporation"
)

// BlueprintOwner is the character or corporation a set of blueprints was
// fetched for.
type BlueprintOwner struct {
	Id   int64
	Type string
	Name string
}

// reactionFormulaGroups are the SDE groups holding reaction formulas, which
// never run out of runs and so are shown like originals.
var reactionFormulaGroups = map[int]bool{
	1888: true, // Composite Reaction Formulas
	1889: true, // Polymer Reaction Formulas
	1890: true, // Biochemical Reaction Formulas
	4097: true, // neurolink enhancer reaction formulas
}

// SaveBlueprintData replaces everything stored for owner with blueprints:
// rows are inserted or updated by item_id, and the owner's rows that are no
// longer in the list (used up, sold, moved away) are deleted. locations,
// from location.Resolver, fills in location_name and container_name; refs
// missing from it (or a nil map) are saved without a location.
func SaveBlueprintData(owner BlueprintOwner, blueprints []esi.Blueprint, locations map[location.Ref]location.Location) error {
	ctx := context.Background()

	conn, err := connectDB()
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())

	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())

	batch := &pgx.Batch{}
	itemIds := make([]int64, 0, len(blueprints))
	for _, blueprint := range blueprints {
		var locationName, containerName *string
		if loc, ok := locations[location.Ref{LocationId: blueprint.LocationId, LocationFlag: blueprint.LocationFlag}]; ok {
			locationName, containerName = &loc.Name, &loc.ContainerName
		}
		batch.Queue(
			`INSERT INTO meadow_works.blueprints
			(item_id, location_flag, location_id, material_efficiency, quantity, runs, time_efficiency, type_id,
			 is_copy, owner_id, owner_type, owner_name, location_name, container_name)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
			ON CONFLICT (item_id) DO UPDATE
			SET location_flag = $2, location_id = $3, material_efficiency = $4, quantity = $5, runs = $6,
			    time_efficiency = $7, type_id = $8, is_copy = $9, owner_id = $10, owner_type = $11, owner_name = $12,
			    location_name = $13, container_name = $14`,
			blueprint.ItemId, blueprint.LocationFlag, blueprint.LocationId, blueprint.MaterialEfficiency,
			blueprint.Quantity, blueprint.Runs, blueprint.TimeEfficiency, blueprint.TypeId,
			blueprint.Quantity == -2, owner.Id, owner.Type, owner.Name, locationName, containerName)
		itemIds = append(itemIds, blueprint.ItemId)
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("saving blueprints for %s: %w", owner.Name, err)
	}

	_, err = tx.Exec(ctx,
		`DELETE FROM meadow_works.blueprints WHERE owner_id = $1 AND NOT (item_id = ANY($2))`,
		owner.Id, itemIds)
	if err != nil {
		return fmt.Errorf("removing old blueprints for %s: %w", owner.Name, err)
	}

	return tx.Commit(ctx)
}

// BlueprintLibraryRow is one line of the Blueprint Library: identical
// blueprints (same type, kind, runs, ME, TE and owner) counted together.
type BlueprintLibraryRow struct {
	TypeId   int
	TypeName string
	// IsOriginal is true for a BPO or a reaction formula, which have
	// unlimited runs.
	IsOriginal         bool
	Runs               int // runs per copy; meaningless when IsOriginal
	Quantity           int
	TotalRuns          int // Runs * Quantity; meaningless when IsOriginal
	MaterialEfficiency int
	TimeEfficiency     int
	OwnerName          string
	// LocationName and ContainerName are only filled in when the library is
	// loaded by location; ContainerName is location.NotInContainer outside one.
	LocationName  string
	ContainerName string
}

// LoadBlueprintLibrary returns the Blueprint Library rows for one owner, or
// for every owner when ownerId is 0, sorted by item name. byLocation also
// splits rows by the station or structure and container they are in.
func LoadBlueprintLibrary(ownerId int64, byLocation bool) ([]BlueprintLibraryRow, error) {
	ctx := context.Background()

	conn, err := connectDB()
	if err != nil {
		return nil, err
	}
	defer conn.Close(context.Background())

	// Blueprints saved before locations were recorded have none yet.
	locationColumns, locationGroup := `'', ''`, ``
	if byLocation {
		locationColumns = `COALESCE(location_name, '` + location.Unknown + ` location'), COALESCE(container_name, '` + location.Unknown + `')`
		locationGroup = `, location_name, container_name`
	}

	// A positive quantity is a stack of that many unused originals.
	rows, err := conn.Query(ctx,
		`SELECT type_id, is_copy, runs, material_efficiency, time_efficiency, COALESCE(owner_name, 'Unknown'),
			`+locationColumns+`, SUM(CASE WHEN quantity > 0 THEN quantity ELSE 1 END)
		FROM meadow_works.blueprints
		WHERE $1 = 0 OR owner_id = $1
		GROUP BY type_id, is_copy, runs, material_efficiency, time_efficiency, owner_id, owner_name`+locationGroup,
		ownerId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items, err := getItems()
	if err != nil {
		return nil, err
	}

	library := make([]BlueprintLibraryRow, 0)
	for rows.Next() {
		var row BlueprintLibraryRow
		var isCopy bool
		if err := rows.Scan(&row.TypeId, &isCopy, &row.Runs, &row.MaterialEfficiency, &row.TimeEfficiency, &row.OwnerName, &row.LocationName, &row.ContainerName, &row.Quantity); err != nil {
			return nil, err
		}

		item, ok := items[row.TypeId]
		row.TypeName = item.Name
		if !ok || row.TypeName == "" {
			row.TypeName = fmt.Sprintf("Unknown type %d", row.TypeId)
		}
		row.IsOriginal = !isCopy || reactionFormulaGroups[item.GroupId]
		if !row.IsOriginal {
			row.TotalRuns = row.Runs * row.Quantity
		}

		library = append(library, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Originals first, then the most researched, then the most runs.
	sort.Slice(library, func(i, j int) bool {
		a, b := library[i], library[j]
		switch {
		case a.TypeName != b.TypeName:
			return a.TypeName < b.TypeName
		case a.OwnerName != b.OwnerName:
			return a.OwnerName < b.OwnerName
		case a.LocationName != b.LocationName:
			return a.LocationName < b.LocationName
		case a.ContainerName != b.ContainerName:
			return a.ContainerName < b.ContainerName
		case a.IsOriginal != b.IsOriginal:
			return a.IsOriginal
		case a.MaterialEfficiency != b.MaterialEfficiency:
			return a.MaterialEfficiency > b.MaterialEfficiency
		case a.TimeEfficiency != b.TimeEfficiency:
			return a.TimeEfficiency > b.TimeEfficiency
		default:
			return a.Runs > b.Runs
		}
	})

	return library, nil
}
