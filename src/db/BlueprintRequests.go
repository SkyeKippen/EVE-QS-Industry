package db

import (
	"QS-Indy/src/esi"
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrBlueprintNotAvailable means the requested blueprints aren't shared any
// more, or don't have that many runs left.
var ErrBlueprintNotAvailable = errors.New("those blueprints don't have that many runs available")

// BlueprintRequest is someone asking an owner for runs of one line of All
// Blueprints (see BlueprintLibraryRow), saved in
// meadow_works.blueprint_requests.
type BlueprintRequest struct {
	RequestId              int64
	RequestedAt            time.Time
	RequesterUserId        int64
	RequesterCharacterId   int64
	RequesterCharacterName string
	OwnerId                int64
	OwnerName              string
	TypeId                 int
	TypeName               string
	IsCopy                 bool
	RunsPerCopy            int
	MaterialEfficiency     int
	TimeEfficiency         int
	RunsRequested          int
	Status                 string
}

// CreateBlueprintRequest saves request after checking that its owner still
// shares matching blueprints with at least RunsRequested runs between them
// (originals and reaction formulas have no limit). It fills in OwnerName,
// TypeName, RequestId, RequestedAt and Status, and returns the saved request.
func CreateBlueprintRequest(ctx context.Context, request BlueprintRequest) (BlueprintRequest, error) {
	if request.RunsRequested < 1 {
		return BlueprintRequest{}, ErrBlueprintNotAvailable
	}

	conn, err := connectDB()
	if err != nil {
		return BlueprintRequest{}, err
	}
	defer conn.Close(context.Background())

	// Counted the same way as LoadBlueprintLibrary counts Quantity.
	var count int
	var ownerName string
	err = conn.QueryRow(ctx,
		`SELECT COALESCE(SUM(CASE WHEN quantity > 0 THEN quantity ELSE 1 END), 0), COALESCE(MAX(owner_name), 'Unknown')
		FROM meadow_works.blueprints
		WHERE is_shared AND owner_id = $1 AND type_id = $2 AND is_copy = $3 AND runs = $4
		  AND material_efficiency = $5 AND time_efficiency = $6`,
		request.OwnerId, request.TypeId, request.IsCopy, request.RunsPerCopy,
		request.MaterialEfficiency, request.TimeEfficiency).Scan(&count, &ownerName)
	if err != nil {
		return BlueprintRequest{}, fmt.Errorf("checking requested blueprints: %w", err)
	}
	if count == 0 {
		return BlueprintRequest{}, ErrBlueprintNotAvailable
	}

	items, err := getItems()
	if err != nil {
		return BlueprintRequest{}, err
	}
	item := items[request.TypeId]
	unlimited := !request.IsCopy || reactionFormulaGroups[item.GroupId]
	if !unlimited && request.RunsRequested > request.RunsPerCopy*count {
		return BlueprintRequest{}, ErrBlueprintNotAvailable
	}
	request.OwnerName = ownerName
	request.TypeName = item.Name
	if request.TypeName == "" {
		request.TypeName = fmt.Sprintf("Unknown type %d", request.TypeId)
	}

	err = conn.QueryRow(ctx,
		`INSERT INTO meadow_works.blueprint_requests
		(requester_user_id, requester_character_id, requester_character_name, owner_id, owner_name,
		 type_id, is_copy, runs_per_copy, material_efficiency, time_efficiency, runs_requested)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING request_id, requested_at, status`,
		request.RequesterUserId, request.RequesterCharacterId, request.RequesterCharacterName,
		request.OwnerId, request.OwnerName, request.TypeId, request.IsCopy, request.RunsPerCopy,
		request.MaterialEfficiency, request.TimeEfficiency, request.RunsRequested).
		Scan(&request.RequestId, &request.RequestedAt, &request.Status)
	if err != nil {
		return BlueprintRequest{}, fmt.Errorf("saving blueprint request: %w", err)
	}
	return request, nil
}

// LoadWaitingBlueprintRequests returns every request whose owner hasn't
// been told about it yet, oldest first, with TypeName filled in.
func LoadWaitingBlueprintRequests(ctx context.Context) ([]BlueprintRequest, error) {
	conn, err := connectDB()
	if err != nil {
		return nil, err
	}
	defer conn.Close(context.Background())

	rows, err := conn.Query(ctx,
		`SELECT request_id, requested_at, requester_user_id, requester_character_id, requester_character_name,
			owner_id, owner_name, type_id, is_copy, runs_per_copy, material_efficiency, time_efficiency,
			runs_requested, status
		FROM meadow_works.blueprint_requests
		WHERE notified_at IS NULL
		ORDER BY requested_at, request_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items, err := getItems()
	if err != nil {
		return nil, err
	}
	requests := make([]BlueprintRequest, 0)
	for rows.Next() {
		var r BlueprintRequest
		if err := rows.Scan(&r.RequestId, &r.RequestedAt, &r.RequesterUserId, &r.RequesterCharacterId, &r.RequesterCharacterName,
			&r.OwnerId, &r.OwnerName, &r.TypeId, &r.IsCopy, &r.RunsPerCopy, &r.MaterialEfficiency, &r.TimeEfficiency,
			&r.RunsRequested, &r.Status); err != nil {
			return nil, err
		}
		r.TypeName = items[r.TypeId].Name
		if r.TypeName == "" {
			r.TypeName = fmt.Sprintf("Unknown type %d", r.TypeId)
		}
		requests = append(requests, r)
	}
	return requests, rows.Err()
}

// MarkBlueprintRequestsNotified records that the owners of these requests
// have been told about them.
func MarkBlueprintRequestsNotified(ctx context.Context, requestIds []int64) error {
	conn, err := connectDB()
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())

	_, err = conn.Exec(ctx,
		`UPDATE meadow_works.blueprint_requests SET notified_at = now() WHERE request_id = ANY($1)`, requestIds)
	return err
}

// BlueprintOwnerUsers returns the app users to tell about requests for each
// of ownerIds' blueprints. A character's blueprints belong to the user it is
// linked to. A corporation's belong to every user with a character in it
// that can share them (granted the corporation blueprints scope, as
// SharableOwners requires). Owners nobody can be found for are left out.
func BlueprintOwnerUsers(ctx context.Context, ownerIds []int64) (map[int64][]int64, error) {
	conn, err := connectDB()
	if err != nil {
		return nil, err
	}
	defer conn.Close(context.Background())

	users := make(map[int64][]int64, len(ownerIds))
	rows, err := conn.Query(ctx,
		`SELECT character_id, user_id FROM meadow_works.user_characters WHERE character_id = ANY($1)`, ownerIds)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var characterId, userId int64
		if err := rows.Scan(&characterId, &userId); err != nil {
			rows.Close()
			return nil, err
		}
		users[characterId] = []int64{userId}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	corporations := make(map[int64]bool)
	for _, id := range ownerIds {
		if _, ok := users[id]; !ok {
			corporations[id] = true
		}
	}
	if len(corporations) == 0 {
		return users, nil
	}

	tokens, err := ListTokenCharacters(ctx)
	if err != nil {
		return nil, err
	}
	for _, token := range tokens {
		if !slices.Contains(token.Scopes, scopeCorporationBlueprints) {
			continue
		}
		corporationId, err := esi.GetCharacterCorporationCached(int(token.CharacterID))
		if err != nil {
			log.Printf("blueprint requests: corporation of character %d: %v", token.CharacterID, err)
			continue
		}
		if !corporations[corporationId] {
			continue
		}
		var userId int64
		err = conn.QueryRow(ctx,
			`SELECT user_id FROM meadow_works.user_characters WHERE character_id = $1`, token.CharacterID).Scan(&userId)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !slices.Contains(users[corporationId], userId) {
			users[corporationId] = append(users[corporationId], userId)
		}
	}
	return users, nil
}
