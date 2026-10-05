// Package location turns the location_id and location_flag that ESI gives
// for a blueprint (or any other asset) into the station or structure it is
// in.
//
// Only items sitting straight in a hangar can be placed. Anything inside a
// container, a ship or a corporation office has that item's ID as its
// location_id, and following it up needs the assets scopes, which the app
// doesn't ask for, so those come back as unknown.
//
// Stations and structures are cached through a Store and re-checked with
// ESI once they are 30 days old. Structures the token can't read are
// reported as unknown rather than failing the lookup.
package location

import (
	"QS-Indy/src/esi"
	"context"
	"errors"
	"log"
	"time"
)

const (
	KindStation   = "station"
	KindStructure = "structure"
	KindSpace     = "space"   // the item is floating in a solar system
	KindUnknown   = "unknown" // in a container, ship or corp office, or ESI couldn't be reached

	Unknown    = "Unknown"
	NPCStation = "NPC Station"
	NPCOwner   = "NPC"

	// how long a cached station or structure is trusted
	RecheckAfter = 30 * 24 * time.Hour
	// how long to wait before asking again about a structure the token
	// couldn't read; 403s count toward ESI's error limit, so don't repeat
	// them on every page load
	RecheckInaccessibleAfter = 24 * time.Hour
)

// Ref is where ESI says an item is.
type Ref struct {
	LocationId   int64
	LocationFlag string
}

// Location is the answer for one Ref.
type Location struct {
	LocationId int64  // the station or structure ID; 0 when unknown
	Kind       string // KindStation, KindStructure, KindSpace or KindUnknown
	Accessible bool   // false when the structure couldn't be read and the names may be missing or out of date
	Region     string
	System     string
	Name       string
	Type       string // the structure type, or NPCStation
	Owner      string // the owning corporation, or NPCOwner
}

// Place is a cached station or structure.
type Place struct {
	LocationId int64
	Kind       string // KindStation or KindStructure
	Accessible bool
	Name       string
	TypeId     int64
	OwnerId    int64
	OwnerName  string
	SystemId   int64
	SystemName string
	RegionId   int64
	RegionName string
	CheckedAt  time.Time
}

// Store caches places; db.LocationStore is the Postgres one.
type Store interface {
	GetPlaces(ctx context.Context, locationIds []int64) (map[int64]Place, error)
	SavePlace(ctx context.Context, place Place) error
}

// ESI is the subset of esi.Client the resolver uses.
type ESI interface {
	GetStation(ctx context.Context, stationId int64) (esi.Station, error)
	GetStructure(ctx context.Context, structureId int64, accessToken string) (esi.Structure, error)
	GetSolarSystem(ctx context.Context, systemId int64) (esi.SolarSystem, error)
	GetConstellation(ctx context.Context, constellationId int64) (esi.Constellation, error)
	GetNames(ctx context.Context, ids []int64) (map[int64]string, error)
}

type Resolver struct {
	ESI   ESI
	Store Store
	// TypeName names a type ID from the SDE, for structure types. It
	// returns "" for unknown types.
	TypeName func(typeId int64) string
	Now      func() time.Time
}

func (r *Resolver) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Resolver) typeName(typeId int64) string {
	if r.TypeName == nil {
		return ""
	}
	return r.TypeName(typeId)
}

// Resolve looks up every ref. accessToken is used for structures, so it
// must belong to a character who can dock there. Only a failing Store
// returns an error; ESI failures leave that ref unknown.
func (r *Resolver) Resolve(ctx context.Context, accessToken string, refs []Ref) (map[Ref]Location, error) {
	placeIds := make([]int64, 0, len(refs))
	for _, ref := range refs {
		if id := placeId(ref); id != 0 {
			placeIds = append(placeIds, id)
		}
	}

	places, err := r.places(ctx, accessToken, placeIds)
	if err != nil {
		return nil, err
	}

	systems := map[int64]systemInfo{}
	locations := make(map[Ref]Location, len(refs))
	for _, ref := range refs {
		if _, done := locations[ref]; done {
			continue
		}
		switch {
		case placeId(ref) != 0:
			locations[ref] = placeLocation(ref.LocationId, places[ref.LocationId], r.typeName)
		case isSolarSystem(ref.LocationId):
			locations[ref] = r.spaceLocation(ctx, ref.LocationId, systems)
		default:
			locations[ref] = unknownLocation(0, KindUnknown)
		}
	}
	return locations, nil
}

// placeId is the station or structure the ref is straight in, or 0 when
// it is in space or inside something else.
func placeId(ref Ref) int64 {
	if isStation(ref.LocationId) {
		return ref.LocationId
	}
	if isHangarFlag(ref.LocationFlag) && !isSolarSystem(ref.LocationId) {
		return ref.LocationId
	}
	return 0
}

// places returns every place, from the cache when it is fresh enough and
// from ESI otherwise.
func (r *Resolver) places(ctx context.Context, accessToken string, placeIds []int64) (map[int64]Place, error) {
	if len(placeIds) == 0 {
		return map[int64]Place{}, nil
	}
	cached, err := r.Store.GetPlaces(ctx, placeIds)
	if err != nil {
		return nil, err
	}

	systems := map[int64]systemInfo{}
	places := make(map[int64]Place, len(placeIds))
	for _, id := range placeIds {
		if _, done := places[id]; done {
			continue
		}
		place, isCached := cached[id]
		if isCached && !r.isStale(place) {
			places[id] = place
			continue
		}

		fresh, err := r.fetchPlace(ctx, accessToken, id, systems)
		if err != nil {
			// a network or ESI outage: keep whatever was cached, and try
			// again next time
			log.Printf("location: looking up %d: %v", id, err)
			if isCached {
				places[id] = place
			}
			continue
		}
		if !fresh.Accessible && isCached && place.Name != "" {
			// keep the last known details of a structure that stopped
			// being readable
			stale := place
			stale.Accessible = false
			stale.CheckedAt = fresh.CheckedAt
			fresh = stale
		}

		if err := r.Store.SavePlace(ctx, fresh); err != nil {
			return nil, err
		}
		places[id] = fresh
	}
	return places, nil
}

func (r *Resolver) isStale(place Place) bool {
	age := r.now().Sub(place.CheckedAt)
	if place.Accessible {
		return age >= RecheckAfter
	}
	return age >= RecheckInaccessibleAfter
}

// fetchPlace asks ESI about a station or structure. A structure the token
// can't read comes back as an inaccessible place, not an error.
func (r *Resolver) fetchPlace(ctx context.Context, accessToken string, id int64, systems map[int64]systemInfo) (Place, error) {
	place := Place{LocationId: id, CheckedAt: r.now()}

	if isStation(id) {
		station, err := r.ESI.GetStation(ctx, id)
		if err != nil {
			return Place{}, err
		}
		place.Kind = KindStation
		place.Accessible = true
		place.Name = station.Name
		place.TypeId = station.TypeId
		place.OwnerId = station.Owner
		place.OwnerName = NPCOwner
		place.SystemId = station.SystemId
	} else {
		structure, err := r.ESI.GetStructure(ctx, id, accessToken)
		if errors.Is(err, esi.ErrForbidden) || errors.Is(err, esi.ErrNotFound) {
			place.Kind = KindStructure
			place.Accessible = false
			return place, nil
		}
		if err != nil {
			return Place{}, err
		}
		place.Kind = KindStructure
		place.Accessible = true
		place.Name = structure.Name
		place.TypeId = structure.TypeId
		place.OwnerId = structure.OwnerId
		place.SystemId = structure.SolarSystemId

		names, err := r.ESI.GetNames(ctx, []int64{structure.OwnerId})
		if err != nil {
			return Place{}, err
		}
		place.OwnerName = names[structure.OwnerId]
	}

	system, err := r.system(ctx, place.SystemId, systems)
	if err != nil {
		return Place{}, err
	}
	place.SystemName = system.name
	place.RegionId = system.regionId
	place.RegionName = system.regionName
	return place, nil
}

type systemInfo struct {
	name       string
	regionId   int64
	regionName string
}

func (r *Resolver) system(ctx context.Context, systemId int64, systems map[int64]systemInfo) (systemInfo, error) {
	if info, ok := systems[systemId]; ok {
		return info, nil
	}
	system, err := r.ESI.GetSolarSystem(ctx, systemId)
	if err != nil {
		return systemInfo{}, err
	}
	constellation, err := r.ESI.GetConstellation(ctx, system.ConstellationId)
	if err != nil {
		return systemInfo{}, err
	}
	names, err := r.ESI.GetNames(ctx, []int64{constellation.RegionId})
	if err != nil {
		return systemInfo{}, err
	}
	info := systemInfo{name: system.Name, regionId: constellation.RegionId, regionName: names[constellation.RegionId]}
	systems[systemId] = info
	return info, nil
}

func (r *Resolver) spaceLocation(ctx context.Context, systemId int64, systems map[int64]systemInfo) Location {
	system, err := r.system(ctx, systemId, systems)
	if err != nil {
		log.Printf("location: looking up system %d: %v", systemId, err)
		return unknownLocation(0, KindSpace)
	}
	return Location{
		Kind:       KindSpace,
		Accessible: true,
		Region:     system.regionName,
		System:     system.name,
		Name:       "In space",
		Type:       Unknown,
		Owner:      Unknown,
	}
}

func placeLocation(id int64, place Place, typeName func(int64) string) Location {
	if place.LocationId == 0 {
		// ESI couldn't be reached and nothing was cached
		return unknownLocation(id, KindUnknown)
	}
	if place.Name == "" {
		return unknownLocation(id, place.Kind)
	}

	location := Location{
		LocationId: id,
		Kind:       place.Kind,
		Accessible: place.Accessible,
		Region:     orUnknown(place.RegionName),
		System:     orUnknown(place.SystemName),
		Name:       place.Name,
		Owner:      orUnknown(place.OwnerName),
	}
	if place.Kind == KindStation {
		location.Type = NPCStation
		location.Owner = NPCOwner
	} else {
		location.Type = orUnknown(typeName(place.TypeId))
	}
	return location
}

func unknownLocation(id int64, kind string) Location {
	name := Unknown + " location"
	if kind == KindStructure {
		name = Unknown + " structure"
	}
	return Location{
		LocationId: id,
		Kind:       kind,
		Region:     Unknown,
		System:     Unknown,
		Name:       name,
		Type:       Unknown,
		Owner:      Unknown,
	}
}

func orUnknown(s string) string {
	if s == "" {
		return Unknown
	}
	return s
}

func isStation(id int64) bool {
	return id >= 60_000_000 && id < 64_000_000
}

func isSolarSystem(id int64) bool {
	return id >= 30_000_000 && id < 33_000_000
}

// isHangarFlag is true for flags whose location_id is the station or
// structure itself.
func isHangarFlag(flag string) bool {
	switch flag {
	case "Hangar", "Deliveries", "CorpDeliveries", "HangarAll":
		return true
	}
	return false
}
