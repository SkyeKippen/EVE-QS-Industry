package location

import (
	"QS-Indy/src/esi"
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"
)

var corpOwner = Owner{CharacterId: 1, CorporationId: myCorp}

var testNow = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

const (
	jitaSystem   = 30000142
	jitaConst    = 20000020
	theForge     = 10000002
	jita44       = 60003760
	caldariNavy  = 1000035
	myStructure  = 1035466617946
	myCorp       = 98000001
	astrahusType = 35832
	stationCont  = 17366
)

type fakeESI struct {
	assets         []esi.Asset
	assetsErr      error
	assetNames     map[int64]string
	forbidden      map[int64]bool
	structureCalls int
	assetCalls     int
}

func (f *fakeESI) GetStation(ctx context.Context, id int64) (esi.Station, error) {
	if id != jita44 {
		return esi.Station{}, esi.ErrNotFound
	}
	return esi.Station{Name: "Jita IV - Moon 4 - Caldari Navy Assembly Plant", Owner: caldariNavy, SystemId: jitaSystem, TypeId: 52678}, nil
}

func (f *fakeESI) GetStructure(ctx context.Context, id int64, token string) (esi.Structure, error) {
	f.structureCalls++
	if f.forbidden[id] || id != myStructure {
		return esi.Structure{}, fmt.Errorf("%w: test", esi.ErrForbidden)
	}
	return esi.Structure{Name: "Jita - Skye's Astrahus", OwnerId: myCorp, SolarSystemId: jitaSystem, TypeId: astrahusType}, nil
}

func (f *fakeESI) GetSolarSystem(ctx context.Context, id int64) (esi.SolarSystem, error) {
	return esi.SolarSystem{Name: "Jita", ConstellationId: jitaConst}, nil
}

func (f *fakeESI) GetConstellation(ctx context.Context, id int64) (esi.Constellation, error) {
	return esi.Constellation{Name: "Kimotoro", RegionId: theForge}, nil
}

func (f *fakeESI) GetNames(ctx context.Context, ids []int64) (map[int64]string, error) {
	all := map[int64]string{theForge: "The Forge", myCorp: "Meadow Works"}
	names := map[int64]string{}
	for _, id := range ids {
		names[id] = all[id]
	}
	return names, nil
}

func (f *fakeESI) GetCorporationAssets(ctx context.Context, corporationId int64, token string) ([]esi.Asset, error) {
	f.assetCalls++
	return f.assets, f.assetsErr
}

func (f *fakeESI) GetCorporationAssetNames(ctx context.Context, corporationId int64, token string, ids []int64) (map[int64]string, error) {
	return f.assetNames, nil
}

type memStore struct {
	places map[int64]Place
	saves  int
}

func (m *memStore) GetPlaces(ctx context.Context, ids []int64) (map[int64]Place, error) {
	found := map[int64]Place{}
	for _, id := range ids {
		if place, ok := m.places[id]; ok {
			found[id] = place
		}
	}
	return found, nil
}

func (m *memStore) SavePlace(ctx context.Context, place Place) error {
	m.saves++
	m.places[place.LocationId] = place
	return nil
}

func newResolver(f *fakeESI, store *memStore) *Resolver {
	return &Resolver{
		ESI:   f,
		Store: store,
		TypeName: func(typeId int64) string {
			return map[int64]string{astrahusType: "Astrahus", stationCont: "Station Container"}[typeId]
		},
		Now: func() time.Time { return testNow },
	}
}

func resolveOne(t *testing.T, r *Resolver, owner Owner, ref Ref) Location {
	t.Helper()
	locations, err := r.Resolve(context.Background(), owner, []Ref{ref})
	if err != nil {
		t.Fatal(err)
	}
	return locations[ref]
}

func TestStationHangar(t *testing.T) {
	f := &fakeESI{}
	store := &memStore{places: map[int64]Place{}}
	got := resolveOne(t, newResolver(f, store), Owner{CharacterId: 1}, Ref{jita44, "Hangar"})

	want := Location{
		LocationId: jita44, Kind: KindStation, Accessible: true,
		Region: "The Forge", System: "Jita", Name: "Jita IV - Moon 4 - Caldari Navy Assembly Plant",
		Type: NPCStation, Owner: NPCOwner, ContainerName: NotInContainer,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	if f.assetCalls != 0 {
		t.Fatal("assets were downloaded for an item sitting in a hangar")
	}
	if store.places[jita44].CheckedAt != testNow {
		t.Fatal("station was not cached")
	}
}

func TestStructureHangar(t *testing.T) {
	f := &fakeESI{}
	store := &memStore{places: map[int64]Place{}}
	got := resolveOne(t, newResolver(f, store), Owner{CharacterId: 1}, Ref{myStructure, "Hangar"})

	if got.Name != "Jita - Skye's Astrahus" || got.Type != "Astrahus" || got.Owner != "Meadow Works" ||
		got.System != "Jita" || got.Region != "The Forge" || got.Kind != KindStructure || got.InContainer {
		t.Fatalf("got %+v", got)
	}
}

func TestFreshCacheSkipsESI(t *testing.T) {
	f := &fakeESI{}
	store := &memStore{places: map[int64]Place{myStructure: {
		LocationId: myStructure, Kind: KindStructure, Accessible: true, Name: "Cached Name",
		TypeId: astrahusType, OwnerName: "Meadow Works", SystemName: "Jita", RegionName: "The Forge",
		CheckedAt: testNow.Add(-29 * 24 * time.Hour),
	}}}
	got := resolveOne(t, newResolver(f, store), Owner{CharacterId: 1}, Ref{myStructure, "Hangar"})

	if got.Name != "Cached Name" || f.structureCalls != 0 || store.saves != 0 {
		t.Fatalf("got %+v after %d structure calls", got, f.structureCalls)
	}
}

func TestStaleCacheIsRechecked(t *testing.T) {
	f := &fakeESI{}
	store := &memStore{places: map[int64]Place{myStructure: {
		LocationId: myStructure, Kind: KindStructure, Accessible: true, Name: "Old Name",
		CheckedAt: testNow.Add(-31 * 24 * time.Hour),
	}}}
	got := resolveOne(t, newResolver(f, store), Owner{CharacterId: 1}, Ref{myStructure, "Hangar"})

	if got.Name != "Jita - Skye's Astrahus" || f.structureCalls != 1 || store.places[myStructure].CheckedAt != testNow {
		t.Fatalf("got %+v after %d structure calls", got, f.structureCalls)
	}
}

func TestForbiddenStructureIsUnknown(t *testing.T) {
	f := &fakeESI{forbidden: map[int64]bool{myStructure: true}}
	store := &memStore{places: map[int64]Place{}}
	r := newResolver(f, store)
	got := resolveOne(t, r, Owner{CharacterId: 1}, Ref{myStructure, "Hangar"})

	if got.Kind != KindStructure || got.Accessible || got.Name != "Unknown structure" || got.System != Unknown {
		t.Fatalf("got %+v", got)
	}
	if store.places[myStructure].Accessible {
		t.Fatal("the 403 was not cached")
	}

	// a second lookup within a day doesn't ask ESI again
	resolveOne(t, r, Owner{CharacterId: 1}, Ref{myStructure, "Hangar"})
	if f.structureCalls != 1 {
		t.Fatalf("structure asked about %d times", f.structureCalls)
	}
}

func TestStructureThatBecameForbiddenKeepsDetails(t *testing.T) {
	f := &fakeESI{forbidden: map[int64]bool{myStructure: true}}
	store := &memStore{places: map[int64]Place{myStructure: {
		LocationId: myStructure, Kind: KindStructure, Accessible: true, Name: "Old Name",
		TypeId: astrahusType, OwnerName: "Meadow Works", SystemName: "Jita", RegionName: "The Forge",
		CheckedAt: testNow.Add(-31 * 24 * time.Hour),
	}}}
	got := resolveOne(t, newResolver(f, store), Owner{CharacterId: 1}, Ref{myStructure, "Hangar"})

	if got.Name != "Old Name" || got.Accessible || got.System != "Jita" {
		t.Fatalf("got %+v", got)
	}
	if saved := store.places[myStructure]; saved.Accessible || saved.CheckedAt != testNow || saved.Name != "Old Name" {
		t.Fatalf("saved %+v", saved)
	}
}

func TestNestedContainersInStructure(t *testing.T) {
	f := &fakeESI{
		assets: []esi.Asset{
			{ItemId: 1001, LocationId: myStructure, LocationFlag: "Hangar", LocationType: "item", TypeId: stationCont},
			{ItemId: 1002, LocationId: 1001, LocationFlag: "Unlocked", LocationType: "item", TypeId: stationCont},
		},
		assetNames: map[int64]string{1001: "BPOs"},
	}
	store := &memStore{places: map[int64]Place{}}
	got := resolveOne(t, newResolver(f, store), corpOwner, Ref{1002, "Unlocked"})

	if got.Name != "Jita - Skye's Astrahus" || !got.InContainer {
		t.Fatalf("got %+v", got)
	}
	// the inner container has no name, so it falls back to its type
	if got.ContainerName != "Station Container" || !reflect.DeepEqual(got.ContainerPath, []string{"BPOs", "Station Container"}) {
		t.Fatalf("got container %q, path %v", got.ContainerName, got.ContainerPath)
	}
}

func TestCorpOfficeIsNotAContainer(t *testing.T) {
	f := &fakeESI{
		assets: []esi.Asset{
			{ItemId: 2001, LocationId: jita44, LocationFlag: "OfficeFolder", LocationType: "station", TypeId: officeTypeId},
		},
	}
	store := &memStore{places: map[int64]Place{}}
	got := resolveOne(t, newResolver(f, store), Owner{CharacterId: 1, CorporationId: myCorp}, Ref{2001, "CorpSAG3"})

	if got.LocationId != jita44 || got.InContainer || got.ContainerName != NotInContainer {
		t.Fatalf("got %+v", got)
	}
}

func TestContainerWithoutAssetsAccess(t *testing.T) {
	f := &fakeESI{assetsErr: fmt.Errorf("%w: missing scope", esi.ErrForbidden)}
	store := &memStore{places: map[int64]Place{}}
	got := resolveOne(t, newResolver(f, store), corpOwner, Ref{1002, "Unlocked"})

	if got.Kind != KindUnknown || !got.InContainer || got.ContainerName != "Unknown container" {
		t.Fatalf("got %+v", got)
	}
	if f.structureCalls != 0 {
		t.Fatal("a container ID was looked up as a structure")
	}
}

func TestAssetsDownloadedOncePerResolve(t *testing.T) {
	f := &fakeESI{
		assets: []esi.Asset{
			{ItemId: 1001, LocationId: jita44, LocationFlag: "Hangar", LocationType: "station", TypeId: stationCont},
			{ItemId: 1003, LocationId: jita44, LocationFlag: "Hangar", LocationType: "station", TypeId: stationCont},
		},
	}
	store := &memStore{places: map[int64]Place{}}
	locations, err := newResolver(f, store).Resolve(context.Background(), corpOwner,
		[]Ref{{1001, "Unlocked"}, {1003, "Locked"}, {jita44, "Hangar"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(locations) != 3 || f.assetCalls != 1 || store.saves != 1 {
		t.Fatalf("%d locations, %d asset downloads, %d saves", len(locations), f.assetCalls, store.saves)
	}
}

func TestCharacterContainerIsUnknownWithoutAssetCalls(t *testing.T) {
	f := &fakeESI{}
	store := &memStore{places: map[int64]Place{}}
	got := resolveOne(t, newResolver(f, store), Owner{CharacterId: 1}, Ref{1051835972076, "Cargo"})

	if got.Kind != KindUnknown || !got.InContainer || got.ContainerName != "Unknown container" {
		t.Fatalf("got %+v", got)
	}
	if f.assetCalls != 0 || f.structureCalls != 0 {
		t.Fatalf("%d asset calls and %d structure calls for a character's container", f.assetCalls, f.structureCalls)
	}
}

func TestCorpOwnedStructureIsAPlaceNotAContainer(t *testing.T) {
	f := &fakeESI{
		assets: []esi.Asset{
			// the corporation's own structure, anchored in space
			{ItemId: myStructure, LocationId: jitaSystem, LocationFlag: "AutoFit", LocationType: "solar_system", TypeId: astrahusType},
			{ItemId: 3001, LocationId: myStructure, LocationFlag: "OfficeFolder", LocationType: "item", TypeId: officeTypeId},
			{ItemId: 3002, LocationId: 3001, LocationFlag: "CorpSAG3", LocationType: "item", TypeId: stationCont},
		},
		assetNames: map[int64]string{3002: "BPC - T2 Rigs"},
	}
	store := &memStore{places: map[int64]Place{}}
	got := resolveOne(t, newResolver(f, store), corpOwner, Ref{3002, "Unlocked"})

	if got.Kind != KindStructure || got.Name != "Jita - Skye's Astrahus" ||
		!reflect.DeepEqual(got.ContainerPath, []string{"BPC - T2 Rigs"}) {
		t.Fatalf("got %+v", got)
	}
}
