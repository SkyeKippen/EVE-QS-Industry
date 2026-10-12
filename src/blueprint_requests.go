package main

import (
	"QS-Indy/src/auth"
	"QS-Indy/src/db"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
)

// handleBlueprintRequest saves a Request Blueprint form from All Blueprints.
// The form names the line of the library (owner, type, kind, runs per copy,
// ME and TE) and how many runs are wanted; the reply is JSON with the
// saved request's ID and runs, or a plain-text error.
func handleBlueprintRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess, _ := auth.CurrentSession(r)

	request := db.BlueprintRequest{
		RequesterUserId:        sess.UserID,
		RequesterCharacterId:   sess.CharacterID,
		RequesterCharacterName: sess.CharacterName,
		IsCopy:                 r.PostFormValue("copy") == "true",
	}
	fields := []struct {
		name string
		set  func(int64)
	}{
		{"owner", func(v int64) { request.OwnerId = v }},
		{"type", func(v int64) { request.TypeId = int(v) }},
		{"runs-per-copy", func(v int64) { request.RunsPerCopy = int(v) }},
		{"me", func(v int64) { request.MaterialEfficiency = int(v) }},
		{"te", func(v int64) { request.TimeEfficiency = int(v) }},
		{"runs", func(v int64) { request.RunsRequested = int(v) }},
	}
	for _, field := range fields {
		value, err := strconv.ParseInt(r.PostFormValue(field.name), 10, 32)
		if err != nil {
			http.Error(w, "invalid blueprint request: "+field.name, http.StatusBadRequest)
			return
		}
		field.set(value)
	}

	saved, err := db.CreateBlueprintRequest(r.Context(), request)
	if errors.Is(err, db.ErrBlueprintNotAvailable) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		log.Printf("blueprint request by %s: %v", sess.CharacterName, err)
		http.Error(w, "could not save your request", http.StatusInternalServerError)
		return
	}
	log.Printf("blueprint request %d: %s asked %s for %d runs of %s",
		saved.RequestId, saved.RequesterCharacterName, saved.OwnerName, saved.RunsRequested, saved.TypeName)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		RequestId int64 `json:"requestId"`
		Runs      int   `json:"runs"`
	}{saved.RequestId, saved.RunsRequested})
}
