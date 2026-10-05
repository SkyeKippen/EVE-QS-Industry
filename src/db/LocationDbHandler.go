package db

import (
	"QS-Indy/src/esi"
	"QS-Indy/src/location"
	"context"
)

// LocationStore caches stations and structures in meadow_works.locations
// (see tools/db/generate_locations_table.sql). It is the location.Store the
// web app and tools use.
type LocationStore struct{}

func (LocationStore) GetPlaces(ctx context.Context, locationIds []int64) (map[int64]location.Place, error) {
	conn, err := connectDB()
	if err != nil {
		return nil, err
	}
	defer conn.Close(context.Background())

	rows, err := conn.Query(ctx,
		`SELECT location_id, kind, accessible, name, type_id, owner_id, owner_name,
		solar_system_id, solar_system_name, region_id, region_name, checked_at
		FROM meadow_works.locations WHERE location_id = ANY($1)`, locationIds)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	places := make(map[int64]location.Place, len(locationIds))
	for rows.Next() {
		var place location.Place
		if err := rows.Scan(&place.LocationId, &place.Kind, &place.Accessible, &place.Name, &place.TypeId,
			&place.OwnerId, &place.OwnerName, &place.SystemId, &place.SystemName,
			&place.RegionId, &place.RegionName, &place.CheckedAt); err != nil {
			return nil, err
		}
		places[place.LocationId] = place
	}
	return places, rows.Err()
}

func (LocationStore) SavePlace(ctx context.Context, place location.Place) error {
	conn, err := connectDB()
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())

	_, err = conn.Exec(ctx,
		`INSERT INTO meadow_works.locations
		(location_id, kind, accessible, name, type_id, owner_id, owner_name,
		solar_system_id, solar_system_name, region_id, region_name, checked_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (location_id) DO UPDATE
		SET kind = $2, accessible = $3, name = $4, type_id = $5, owner_id = $6, owner_name = $7,
		solar_system_id = $8, solar_system_name = $9, region_id = $10, region_name = $11, checked_at = $12`,
		place.LocationId, place.Kind, place.Accessible, place.Name, place.TypeId, place.OwnerId, place.OwnerName,
		place.SystemId, place.SystemName, place.RegionId, place.RegionName, place.CheckedAt)
	return err
}

// TypeName names a type from the SDE, or returns "" when it isn't found.
// It matches location.Resolver's TypeName.
func TypeName(typeId int64) string {
	items, err := getItems()
	if err != nil {
		return ""
	}
	return items[int(typeId)].Name
}

// NewLocationResolver builds a resolver backed by ESI, the locations table
// and the SDE.
func NewLocationResolver() *location.Resolver {
	return &location.Resolver{
		ESI:      &esi.Client{},
		Store:    LocationStore{},
		TypeName: TypeName,
	}
}
