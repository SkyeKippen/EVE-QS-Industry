package location

import (
	"QS-Indy/src/esi"
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"
)

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
)

type fakeESI struct {
	forbidden      map[int64]bool
	structureCalls int
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
			return map[int64]string{astrahusType: "Astrahus"}[typeId]
		},
		Now: func() time.Time { return testNow },
	}
}

func resolveOne(t *testing.T, r *Resolver, ref Ref) Location {
	t.Helper()
	locations, err := r.Resolve(context.Background(), "token", []Ref{ref})
	if err != nil {
		t.Fatal(err)
	}
	return locations[ref]
}

func TestStationHangar(t *testing.T) {
	store := &memStore{places: map[int64]Place{}}
	got := resolveOne(t, newResolver(&fakeESI{}, store), Ref{jita44, "Hangar"})

	want := Location{
		LocationId: jita44, Kind: KindStation, Accessible: true,
		Region: "The Forge", System: "Jita", Name: "Jita IV - Moon 4 - Caldari Navy Assembly Plant",
		Type: NPCStation, Owner: NPCOwner,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	if store.places[jita44].CheckedAt != testNow {
		t.Fatal("station was not cached")
	}
}

func TestStructureHangar(t *testing.T) {
	store := &memStore{places: map[int64]Place{}}
	got := resolveOne(t, newResolver(&fakeESI{}, store), Ref{myStructure, "Hangar"})

	if got.Name != "Jita - Skye's Astrahus" || got.Type != "Astrahus" || got.Owner != "Meadow Works" ||
		got.System != "Jita" || got.Region != "The Forge" || got.Kind != KindStructure || !got.Accessible {
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
	got := resolveOne(t, newResolver(f, store), Ref{myStructure, "Hangar"})

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
	got := resolveOne(t, newResolver(f, store), Ref{myStructure, "Hangar"})

	if got.Name != "Jita - Skye's Astrahus" || f.structureCalls != 1 || store.places[myStructure].CheckedAt != testNow {
		t.Fatalf("got %+v after %d structure calls", got, f.structureCalls)
	}
}

func TestForbiddenStructureIsUnknown(t *testing.T) {
	f := &fakeESI{forbidden: map[int64]bool{myStructure: true}}
	store := &memStore{places: map[int64]Place{}}
	r := newResolver(f, store)
	got := resolveOne(t, r, Ref{myStructure, "Hangar"})

	if got.Kind != KindStructure || got.Accessible || got.Name != "Unknown structure" || got.System != Unknown {
		t.Fatalf("got %+v", got)
	}
	if store.places[myStructure].Accessible {
		t.Fatal("the 403 was not cached")
	}

	// a second lookup within a day doesn't ask ESI again
	resolveOne(t, r, Ref{myStructure, "Hangar"})
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
	got := resolveOne(t, newResolver(f, store), Ref{myStructure, "Hangar"})

	if got.Name != "Old Name" || got.Accessible || got.System != "Jita" {
		t.Fatalf("got %+v", got)
	}
	if saved := store.places[myStructure]; saved.Accessible || saved.CheckedAt != testNow || saved.Name != "Old Name" {
		t.Fatalf("saved %+v", saved)
	}
}

func TestItemsInsideSomethingAreUnknownWithoutLookups(t *testing.T) {
	f := &fakeESI{}
	store := &memStore{places: map[int64]Place{}}
	for _, ref := range []Ref{{1051835972076, "Cargo"}, {1049781399349, "CorpSAG3"}, {1049781399350, "Unlocked"}} {
		got := resolveOne(t, newResolver(f, store), ref)
		if got.Kind != KindUnknown || got.Name != "Unknown location" {
			t.Fatalf("%+v: got %+v", ref, got)
		}
	}
	if f.structureCalls != 0 || store.saves != 0 {
		t.Fatalf("%d structure calls and %d saves for items in containers", f.structureCalls, store.saves)
	}
}

func TestEachPlaceLookedUpOncePerResolve(t *testing.T) {
	f := &fakeESI{}
	store := &memStore{places: map[int64]Place{}}
	refs := []Ref{{myStructure, "Hangar"}, {myStructure, "Deliveries"}, {jita44, "Hangar"}, {jita44, "Hangar"}}
	locations, err := newResolver(f, store).Resolve(context.Background(), "token", refs)
	if err != nil {
		t.Fatal(err)
	}
	if len(locations) != 3 || f.structureCalls != 1 || store.saves != 2 {
		t.Fatalf("%d locations, %d structure calls, %d saves", len(locations), f.structureCalls, store.saves)
	}
}
