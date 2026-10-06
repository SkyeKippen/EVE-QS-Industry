// Package location turns the location_id and location_flag that ESI gives
// for a blueprint (or any other asset) into the station or structure it is
// in, and the container it is in, if any. Containers are only followed for
// corporation items (see Owner).
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
	KindUnknown   = "unknown" // the chain couldn't be followed

	Unknown        = "Unknown"
	NotInContainer = "N/A"
	NPCStation     = "NPC Station"
	NPCOwner       = "NPC"

	// how long a cached station or structure is trusted
	RecheckAfter = 30 * 24 * time.Hour
	// how long to wait before asking again about a structure the token
	// couldn't read; 403s count toward ESI's error limit, so don't repeat
	// them on every page load
	RecheckInaccessibleAfter = 24 * time.Hour

	officeTypeId = 27 // corporation offices hold the corp hangars; not a container to the user
	maxHops      = 10
)

// Ref is where ESI says an item is.
type Ref struct {
	LocationId   int64
	LocationFlag string
}

// Owner is whose items are being looked up. CorporationId is 0 for a
// character's own items. Only corporation assets are read (the app asks for
// esi-assets.read_corporation_assets.v1 but not the character assets
// scope), so a character's items inside a container or ship come back as
// in an unknown container at an unknown location. The access token belongs
// to CharacterId either way.
type Owner struct {
	CharacterId   int64
	CorporationId int64
	AccessToken   string
}

// Location is the answer for one Ref.
type Location struct {
	LocationId    int64  // the station or structure ID; 0 when unknown
	Kind          string // KindStation, KindStructure, KindSpace or KindUnknown
	Accessible    bool   // false when the structure couldn't be read and the names may be missing or out of date
	Region        string
	System        string
	Name          string
	Type          string // the structure type, or NPCStation
	Owner         string // the owning corporation, or NPCOwner
	// Hangar is the hangar the item (or its outermost container) sits in:
	// a corporation division such as CorpSAG3, or Hangar for a character's
	// own hangar. It is "" when the chain didn't reach one.
	Hangar        string
	InContainer   bool
	ContainerId   int64    // the innermost container's item ID, or 0
	ContainerName string   // the innermost container, or NotInContainer
	ContainerPath []string // every container, outermost first
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
	GetCorporationAssets(ctx context.Context, corporationId int64, accessToken string) ([]esi.Asset, error)
	GetCorporationAssetNames(ctx context.Context, corporationId int64, accessToken string, itemIds []int64) (map[int64]string, error)
}

type Resolver struct {
	ESI   ESI
	Store Store
	// TypeName names a type ID from the SDE, for structure types and
	// unnamed containers. It returns "" for unknown types.
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

// walk is how far one Ref's location chain could be followed.
type walk struct {
	placeId    int64   // station or structure, 0 if none
	systemId   int64   // set for KindSpace
	hangar     string  // the last hangar flag seen on the way up
	containers []int64 // innermost first
}

// Resolve looks up every ref for one owner. Assets are only fetched if a
// ref needs its container chain walked, and only once per call. Only a
// failing Store returns an error; ESI failures leave that ref unknown.
func (r *Resolver) Resolve(ctx context.Context, owner Owner, refs []Ref) (map[Ref]Location, error) {
	assets := &assetIndex{resolver: r, owner: owner}

	walks := make(map[Ref]walk, len(refs))
	placeIds := make([]int64, 0, len(refs))
	for _, ref := range refs {
		if _, done := walks[ref]; done {
			continue
		}
		w := r.walk(ctx, ref, assets)
		walks[ref] = w
		if w.placeId != 0 {
			placeIds = append(placeIds, w.placeId)
		}
	}

	places, err := r.places(ctx, owner, placeIds)
	if err != nil {
		return nil, err
	}

	containerNames := r.containerNames(ctx, owner, walks, assets)
	systems := map[int64]systemInfo{}

	locations := make(map[Ref]Location, len(walks))
	for ref, w := range walks {
		var location Location
		switch {
		case w.placeId != 0:
			location = placeLocation(w.placeId, places[w.placeId], r.typeName)
		case w.systemId != 0:
			location = r.spaceLocation(ctx, w.systemId, systems)
		default:
			location = unknownLocation(0, KindUnknown)
		}

		location.Hangar = w.hangar
		location.ContainerName = NotInContainer
		if len(w.containers) > 0 {
			location.InContainer = true
			location.ContainerId = w.containers[0]
			for i := len(w.containers) - 1; i >= 0; i-- {
				location.ContainerPath = append(location.ContainerPath, containerNames[w.containers[i]])
			}
			location.ContainerName = containerNames[w.containers[0]]
		}
		locations[ref] = location
	}
	return locations, nil
}

// walk follows the ref up through containers until it reaches a station,
// structure or solar system.
func (r *Resolver) walk(ctx context.Context, ref Ref, assets *assetIndex) walk {
	var w walk
	id, flag := ref.LocationId, ref.LocationFlag
	cameFromAsset := false

	for hop := 0; hop < maxHops; hop++ {
		if isHangarFlag(flag) || isCorpHangarFlag(flag) {
			w.hangar = flag
		}
		if isStation(id) {
			w.placeId = id
			return w
		}
		if isSolarSystem(id) {
			w.systemId = id
			return w
		}
		// an item straight in a hangar is in a station or structure, so
		// there's no need to download every asset to find that out
		if !cameFromAsset && isHangarFlag(flag) {
			w.placeId = id
			return w
		}

		asset, found := assets.get(ctx, id)
		if !found {
			// the parent of an asset that isn't itself an asset is a
			// structure; without the assets list there's no telling
			// whether id is a structure or a container
			if cameFromAsset || isCorpHangarFlag(flag) {
				w.placeId = id
			} else {
				w.containers = append(w.containers, id)
			}
			return w
		}

		// a structure the corporation owns is itself one of its assets,
		// anchored in a solar system; what led here was a hangar or
		// office, not a container's contents
		if isSolarSystem(asset.LocationId) && (isHangarFlag(flag) || isCorpHangarFlag(flag) || flag == "OfficeFolder") {
			w.placeId = id
			return w
		}

		if asset.TypeId != officeTypeId {
			w.containers = append(w.containers, id)
		}
		id, flag = asset.LocationId, asset.LocationFlag
		cameFromAsset = true
	}

	log.Printf("location: gave up after %d hops following location %d", maxHops, ref.LocationId)
	return w
}

// places returns every place, from the cache when it is fresh enough and
// from ESI otherwise.
func (r *Resolver) places(ctx context.Context, owner Owner, placeIds []int64) (map[int64]Place, error) {
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

		fresh, err := r.fetchPlace(ctx, owner, id, systems)
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
func (r *Resolver) fetchPlace(ctx context.Context, owner Owner, id int64, systems map[int64]systemInfo) (Place, error) {
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
		structure, err := r.ESI.GetStructure(ctx, id, owner.AccessToken)
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

// containerNames names every container in walks: the player-given name
// when there is one, otherwise the container's type, such as "Station
// Container".
func (r *Resolver) containerNames(ctx context.Context, owner Owner, walks map[Ref]walk, assets *assetIndex) map[int64]string {
	ids := make([]int64, 0)
	for _, w := range walks {
		ids = append(ids, w.containers...)
	}
	names := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return names
	}

	var given map[int64]string
	if owner.CorporationId != 0 {
		var err error
		given, err = r.ESI.GetCorporationAssetNames(ctx, owner.CorporationId, owner.AccessToken, ids)
		if err != nil {
			log.Printf("location: looking up container names: %v", err)
		}
	}

	for _, id := range ids {
		name := given[id]
		if name == "" {
			if asset, ok := assets.lookup(id); ok {
				name = r.typeName(asset.TypeId)
			}
		}
		if name == "" {
			name = Unknown + " container"
		}
		names[id] = name
	}
	return names
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

// assetIndex downloads the owner's assets the first time one is needed.
type assetIndex struct {
	resolver *Resolver
	owner    Owner
	loaded   bool
	byId     map[int64]esi.Asset
}

func (a *assetIndex) get(ctx context.Context, itemId int64) (esi.Asset, bool) {
	if !a.loaded {
		a.loaded = true
		var assets []esi.Asset
		if a.owner.CorporationId != 0 {
			var err error
			assets, err = a.resolver.ESI.GetCorporationAssets(ctx, a.owner.CorporationId, a.owner.AccessToken)
			if err != nil {
				// usually a missing scope or Director role; containers
				// then can't be followed and those items come back unknown
				log.Printf("location: loading corporation assets: %v", err)
			}
		}
		a.byId = make(map[int64]esi.Asset, len(assets))
		for _, asset := range assets {
			a.byId[asset.ItemId] = asset
		}
	}
	return a.lookup(itemId)
}

// lookup only checks assets that were already loaded.
func (a *assetIndex) lookup(itemId int64) (esi.Asset, bool) {
	asset, ok := a.byId[itemId]
	return asset, ok
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

// isCorpHangarFlag is true for a corporation hangar division, whose
// location_id is the corporation's office (or, for some structures, the
// structure itself).
func isCorpHangarFlag(flag string) bool {
	switch flag {
	case "CorpSAG1", "CorpSAG2", "CorpSAG3", "CorpSAG4", "CorpSAG5", "CorpSAG6", "CorpSAG7":
		return true
	}
	return false
}
