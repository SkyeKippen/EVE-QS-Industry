package db

import (
	"QS-Indy/src/esi"
	"QS-Indy/src/location"
	"context"
	"fmt"
	"log"
	"slices"
	"sort"
	"strconv"
)

// SharingCounts is how many blueprints sit under one part of the Share
// Blueprints tree and how many of those are in the library.
type SharingCounts struct {
	Total  int
	Shared int
}

// AnyShared is true when at least one blueprint under this part of the tree
// is shared, which turns its Add button into Remove.
func (c SharingCounts) AnyShared() bool { return c.Shared > 0 }

func (c *SharingCounts) add(other SharingCounts) {
	c.Total += other.Total
	c.Shared += other.Shared
}

// SharingTree is everything the signed-in user can share, by station or
// structure, then office (a hangar of one owner), then container.
type SharingTree struct {
	SharingCounts
	Structures []*SharingStructure
}

type SharingStructure struct {
	SharingCounts
	PlaceId int64 // 0 for blueprints saved before locations were recorded
	Name    string
	Offices []*SharingOffice
}

type SharingOffice struct {
	SharingCounts
	OwnerId    int64
	OwnerName  string
	Hangar     string // "" when the hangar isn't known
	Name       string
	Containers []*SharingContainer
}

type SharingContainer struct {
	SharingCounts
	ContainerId int64 // 0 for blueprints sitting straight in the hangar
	Name        string
}

// Levels of the Share Blueprints tree a SharingSelection can pick.
const (
	ShareAll       = "all"
	ShareStructure = "structure"
	ShareOffice    = "office"
	ShareContainer = "container"
)

// SharingSelection picks the blueprints an Add or Remove button acts on.
// Level says which of the other fields count: a structure needs PlaceId, an
// office adds OwnerId and Hangar, and a container adds ContainerId.
type SharingSelection struct {
	Level       string
	PlaceId     int64
	OwnerId     int64
	Hangar      string
	ContainerId int64
}

// SharableOwners returns the IDs whose blueprints the user may share: each
// of their characters, and the corporation of each character that granted
// the corporation blueprints scope (which needs the Director role to work).
func SharableOwners(ctx context.Context, userID int64) ([]int64, error) {
	characters, err := ListUserCharacters(ctx, userID)
	if err != nil {
		return nil, err
	}
	tokens, err := ListTokenCharacters(ctx)
	if err != nil {
		return nil, err
	}
	scopes := make(map[int64][]string, len(tokens))
	for _, token := range tokens {
		scopes[token.CharacterID] = token.Scopes
	}

	owners := make([]int64, 0, len(characters)*2)
	for _, character := range characters {
		owners = append(owners, character.CharacterID)
		if !slices.Contains(scopes[character.CharacterID], scopeCorporationBlueprints) {
			continue
		}
		corporationId, err := esi.GetCharacterCorporationCached(int(character.CharacterID))
		if err != nil {
			// leave that corporation out rather than failing the whole page
			log.Printf("sharing: corporation of character %d: %v", character.CharacterID, err)
			continue
		}
		if !slices.Contains(owners, corporationId) {
			owners = append(owners, corporationId)
		}
	}
	return owners, nil
}

// LoadSharingTree builds the Share Blueprints tree for the blueprints of
// ownerIds, sorted by name at each level.
func LoadSharingTree(ctx context.Context, ownerIds []int64) (SharingTree, error) {
	var tree SharingTree

	conn, err := connectDB()
	if err != nil {
		return tree, err
	}
	defer conn.Close(context.Background())

	rows, err := conn.Query(ctx,
		`SELECT COALESCE(place_id, 0),
			CASE WHEN place_id IS NULL THEN 'Location not saved yet' ELSE COALESCE(location_name, '`+location.Unknown+` location') END,
			owner_id, COALESCE(owner_name, 'Unknown'), COALESCE(hangar, ''), COALESCE(hangar_name, '`+location.Unknown+` hangar'),
			COALESCE(container_id, 0),
			CASE WHEN container_id IS NULL THEN 'Not in a container' ELSE COALESCE(container_name, '`+location.Unknown+` container') END,
			COUNT(*), COUNT(*) FILTER (WHERE is_shared)
		FROM meadow_works.blueprints
		WHERE owner_id = ANY($1)
		GROUP BY 1, 2, 3, 4, 5, 6, 7, 8`,
		ownerIds)
	if err != nil {
		return tree, err
	}
	defer rows.Close()

	structures := map[int64]*SharingStructure{}
	offices := map[string]*SharingOffice{}
	containers := map[string]*SharingContainer{}
	for rows.Next() {
		var placeId, ownerId, containerId int64
		var placeName, ownerName, hangar, hangarName, containerName string
		var counts SharingCounts
		if err := rows.Scan(&placeId, &placeName, &ownerId, &ownerName, &hangar, &hangarName,
			&containerId, &containerName, &counts.Total, &counts.Shared); err != nil {
			return tree, err
		}

		structure, ok := structures[placeId]
		if !ok {
			structure = &SharingStructure{PlaceId: placeId, Name: placeName}
			structures[placeId] = structure
			tree.Structures = append(tree.Structures, structure)
		}

		officeKey := fmt.Sprintf("%d/%d/%s", placeId, ownerId, hangar)
		office, ok := offices[officeKey]
		if !ok {
			office = &SharingOffice{OwnerId: ownerId, OwnerName: ownerName, Hangar: hangar, Name: hangarName}
			offices[officeKey] = office
			structure.Offices = append(structure.Offices, office)
		}

		// the same container can come back under several names while an
		// older refresh is still saved, so key it by ID only
		containerKey := officeKey + "/" + strconv.FormatInt(containerId, 10)
		container, ok := containers[containerKey]
		if !ok {
			container = &SharingContainer{ContainerId: containerId, Name: containerName}
			containers[containerKey] = container
			office.Containers = append(office.Containers, container)
		}

		container.add(counts)
		office.add(counts)
		structure.add(counts)
		tree.add(counts)
	}
	if err := rows.Err(); err != nil {
		return tree, err
	}

	// unlocated blueprints go last, as do loose blueprints in an office
	sort.Slice(tree.Structures, func(i, j int) bool {
		a, b := tree.Structures[i], tree.Structures[j]
		if (a.PlaceId == 0) != (b.PlaceId == 0) {
			return b.PlaceId == 0
		}
		return a.Name < b.Name
	})
	for _, structure := range tree.Structures {
		sort.Slice(structure.Offices, func(i, j int) bool {
			a, b := structure.Offices[i], structure.Offices[j]
			if a.OwnerName != b.OwnerName {
				return a.OwnerName < b.OwnerName
			}
			return a.Hangar < b.Hangar
		})
		for _, office := range structure.Offices {
			sort.Slice(office.Containers, func(i, j int) bool {
				a, b := office.Containers[i], office.Containers[j]
				if (a.ContainerId == 0) != (b.ContainerId == 0) {
					return b.ContainerId == 0
				}
				return a.Name < b.Name
			})
		}
	}
	return tree, nil
}

// SetBlueprintsShared adds the selected blueprints of ownerIds to the
// library, or removes them when shared is false. It returns how many
// blueprints it changed.
func SetBlueprintsShared(ctx context.Context, ownerIds []int64, selection SharingSelection, shared bool) (int64, error) {
	conn, err := connectDB()
	if err != nil {
		return 0, err
	}
	defer conn.Close(context.Background())

	query := `UPDATE meadow_works.blueprints SET is_shared = $1 WHERE owner_id = ANY($2) AND is_shared <> $1`
	args := []any{shared, ownerIds}
	where := func(condition string, value any) {
		args = append(args, value)
		query += fmt.Sprintf(" AND "+condition, len(args))
	}

	switch selection.Level {
	case ShareAll:
	case ShareContainer:
		where("COALESCE(container_id, 0) = $%d", selection.ContainerId)
		fallthrough
	case ShareOffice:
		where("owner_id = $%d", selection.OwnerId)
		where("COALESCE(hangar, '') = $%d", selection.Hangar)
		fallthrough
	case ShareStructure:
		where("COALESCE(place_id, 0) = $%d", selection.PlaceId)
	default:
		return 0, fmt.Errorf("unknown sharing level %q", selection.Level)
	}

	tag, err := conn.Exec(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
