package db

import (
	"context"
	"errors"
	"fmt"
	"time"
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
