package db

import (
	"QS-Indy/src/esi"
	"QS-Indy/src/location"
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"sync"
	"time"
)

// Scopes the blueprint refresh needs. The corporation assets scope is
// optional: without it, blueprints in containers come back at an unknown
// location.
const (
	scopeCharacterBlueprints   = "esi-characters.read_blueprints.v1"
	scopeCorporationBlueprints = "esi-corporations.read_blueprints.v1"
)

// a refresh fetches every page and walks corporation assets, so allow it a while
const blueprintRefreshTimeout = 5 * time.Minute

var refreshesRunning sync.Map // character ID -> true while that character's refresh runs

// RefreshBlueprintsInBackground starts RefreshBlueprints for the character
// without waiting for it, and logs the outcome. A refresh already running for
// the same character is left to finish instead of starting another.
func RefreshBlueprintsInBackground(cfg *esi.Config, character TokenCharacter) {
	if _, running := refreshesRunning.LoadOrStore(character.CharacterID, true); running {
		log.Printf("blueprint refresh: already running for %s (%d)", character.CharacterName, character.CharacterID)
		return
	}

	go func() {
		defer refreshesRunning.Delete(character.CharacterID)

		ctx, cancel := context.WithTimeout(context.Background(), blueprintRefreshTimeout)
		defer cancel()

		start := time.Now()
		if err := RefreshBlueprints(ctx, cfg, character); err != nil {
			log.Printf("blueprint refresh: %s (%d): %v", character.CharacterName, character.CharacterID, err)
			return
		}
		log.Printf("blueprint refresh: %s (%d) done in %s", character.CharacterName, character.CharacterID, time.Since(start).Round(time.Millisecond))
	}()
}

// RefreshBlueprints fetches the character's own blueprints and their
// corporation's, works out where each one is, and saves them. Each half is
// skipped when the character didn't grant its scope, leaving whatever was
// saved before in place. A failure in one half doesn't stop the other.
func RefreshBlueprints(ctx context.Context, cfg *esi.Config, character TokenCharacter) error {
	accessToken, err := GetAccessToken(ctx, cfg, character.CharacterID)
	if err != nil {
		return err
	}

	return errors.Join(
		refreshCharacterBlueprints(ctx, character, accessToken),
		refreshCorporationBlueprints(ctx, character, accessToken),
	)
}

func refreshCharacterBlueprints(ctx context.Context, character TokenCharacter, accessToken string) error {
	if !slices.Contains(character.Scopes, scopeCharacterBlueprints) {
		return nil
	}

	blueprints, err := esi.QueryCharacterBlueprints(int(character.CharacterID), accessToken)
	if err != nil {
		return fmt.Errorf("fetching character blueprints: %w", err)
	}
	owner := BlueprintOwner{Id: character.CharacterID, Type: OwnerCharacter, Name: character.CharacterName}
	locationOwner := location.Owner{CharacterId: character.CharacterID, AccessToken: accessToken}
	return saveWithLocations(ctx, owner, locationOwner, blueprints)
}

func refreshCorporationBlueprints(ctx context.Context, character TokenCharacter, accessToken string) error {
	if !slices.Contains(character.Scopes, scopeCorporationBlueprints) {
		return nil
	}

	corporationId, err := esi.GetCharacterCorporationCached(int(character.CharacterID))
	if err != nil {
		return fmt.Errorf("looking up corporation: %w", err)
	}
	// needs the Director role in game; without it ESI answers 403
	blueprints, err := esi.QueryCorporationBlueprints(corporationId, accessToken)
	if err != nil {
		return fmt.Errorf("fetching corporation blueprints: %w", err)
	}
	names, err := esi.GetNames([]int64{corporationId})
	if err != nil {
		return fmt.Errorf("naming corporation: %w", err)
	}
	owner := BlueprintOwner{Id: corporationId, Type: OwnerCorporation, Name: names[corporationId]}
	locationOwner := location.Owner{CharacterId: character.CharacterID, CorporationId: corporationId, AccessToken: accessToken}
	return saveWithLocations(ctx, owner, locationOwner, blueprints)
}

func saveWithLocations(ctx context.Context, owner BlueprintOwner, locationOwner location.Owner, blueprints []esi.Blueprint) error {
	refs := make([]location.Ref, len(blueprints))
	for i, bp := range blueprints {
		refs[i] = location.Ref{LocationId: bp.LocationId, LocationFlag: bp.LocationFlag}
	}
	locations, err := NewLocationResolver().Resolve(ctx, locationOwner, refs)
	if err != nil {
		return fmt.Errorf("locating blueprints for %s: %w", owner.Name, err)
	}

	if err := SaveBlueprintData(owner, blueprints, locations); err != nil {
		return err
	}
	log.Printf("blueprint refresh: saved %d blueprints for %s", len(blueprints), owner.Name)
	return nil
}
