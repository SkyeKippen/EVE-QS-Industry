package main

import (
	"QS-Indy/src/auth"
	"QS-Indy/src/db"
	"QS-Indy/src/notify"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
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

// blueprintRequestWait is how long requests collect after the first one
// before the owner gets them all in one DM, so a run of requests doesn't
// send a message each.
const blueprintRequestWait = 15 * time.Minute

// sendBlueprintRequestNotificationsEvery checks for collected requests that
// are due every interval. Waiting requests are kept in the database, so a
// restart only delays them.
func sendBlueprintRequestNotificationsEvery(interval time.Duration) {
	for {
		time.Sleep(interval)
		if err := sendDueBlueprintRequestNotifications(time.Now()); err != nil {
			log.Printf("blueprint request notifications: %v", err)
		}
	}
}

func sendDueBlueprintRequestNotifications(now time.Time) error {
	if !notify.Enabled() {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	waiting, err := db.LoadWaitingBlueprintRequests(ctx)
	if err != nil || len(waiting) == 0 {
		return err
	}
	ownerIds := make([]int64, 0, len(waiting))
	for _, request := range waiting {
		if !slices.Contains(ownerIds, request.OwnerId) {
			ownerIds = append(ownerIds, request.OwnerId)
		}
	}
	ownerUsers, err := db.BlueprintOwnerUsers(ctx, ownerIds)
	if err != nil {
		return err
	}

	batches, done := dueBlueprintRequestBatches(waiting, ownerUsers, now.Add(-blueprintRequestWait))
	if len(done) == 0 {
		return nil
	}
	// Marked first, so a failure here can't send the same requests twice.
	if err := db.MarkBlueprintRequestsNotified(ctx, done); err != nil {
		return err
	}
	for userId, requests := range batches {
		notify.Send(blueprintRequestsNotification(userId, requests, now))
	}
	return nil
}

// dueBlueprintRequestBatches picks which waiting requests (oldest first) go
// out now. Each user's requests go together once their oldest was made at
// or before cutoff. A request several users are told about (a corporation's
// blueprints) goes to all of them at once, so it can't be marked done
// before one of them has been sent it. Requests nobody can be told about
// are done once they are old enough. It returns each user's requests and
// the IDs of every request that is done.
func dueBlueprintRequestBatches(waiting []db.BlueprintRequest, ownerUsers map[int64][]int64, cutoff time.Time) (map[int64][]db.BlueprintRequest, []int64) {
	byUser := make(map[int64][]db.BlueprintRequest)
	due := make(map[int64]bool)
	for _, request := range waiting {
		for _, userId := range ownerUsers[request.OwnerId] {
			byUser[userId] = append(byUser[userId], request)
			if !request.RequestedAt.After(cutoff) {
				due[userId] = true
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for _, request := range waiting {
			users := ownerUsers[request.OwnerId]
			if !slices.ContainsFunc(users, func(u int64) bool { return due[u] }) {
				continue
			}
			for _, userId := range users {
				if !due[userId] {
					due[userId], changed = true, true
				}
			}
		}
	}

	batches := make(map[int64][]db.BlueprintRequest)
	var done []int64
	for _, request := range waiting {
		users := ownerUsers[request.OwnerId]
		switch {
		case len(users) == 0 && !request.RequestedAt.After(cutoff):
			done = append(done, request.RequestId)
		case len(users) > 0 && due[users[0]]:
			done = append(done, request.RequestId)
		}
	}
	for userId := range due {
		batches[userId] = byUser[userId]
	}
	return batches, done
}

// maxRequestListLength keeps the request list inside Discord's embed
// description limit, with room for the "more" line.
const maxRequestListLength = 3800

// blueprintRequestsNotification lists requests for one user's blueprints,
// grouped by who asked, in the order they were made.
func blueprintRequestsNotification(userId int64, requests []db.BlueprintRequest, now time.Time) notify.Notification {
	title := "New blueprint request"
	if len(requests) > 1 {
		title = commaInt(len(requests)) + " new blueprint requests"
	}

	var requesters []string
	byRequester := make(map[string][]db.BlueprintRequest)
	totalRuns := 0
	for _, request := range requests {
		name := request.RequesterCharacterName
		if _, ok := byRequester[name]; !ok {
			requesters = append(requesters, name)
		}
		byRequester[name] = append(byRequester[name], request)
		totalRuns += request.RunsRequested
	}

	var list strings.Builder
	listed := 0
list:
	for _, name := range requesters {
		section := "**" + name + "** asked for:\n"
		for _, request := range byRequester[name] {
			line := "- " + blueprintRequestLine(request) + "\n"
			if list.Len()+len(section)+len(line) > maxRequestListLength {
				break list
			}
			list.WriteString(section)
			section = ""
			list.WriteString(line)
			listed++
		}
	}
	if more := len(requests) - listed; more > 0 {
		fmt.Fprintf(&list, "…and %s more", commaInt(more))
	}

	n := notify.Notification{
		Event:       notify.EventBlueprintRequested,
		UserID:      userId,
		Title:       title,
		Description: strings.TrimSpace(list.String()),
		Color:       notify.ColorInfo,
		Fields: []notify.Field{
			{Name: "Requests", Value: commaInt(len(requests)), Inline: true},
			{Name: "Total runs", Value: commaInt(totalRuns), Inline: true},
			{Name: "Pilots", Value: commaInt(len(requesters)), Inline: true},
		},
		Footer:    siteName,
		Timestamp: now,
	}
	if sameType := !slices.ContainsFunc(requests, func(r db.BlueprintRequest) bool { return r.TypeId != requests[0].TypeId }); sameType {
		n.ThumbnailURL = fmt.Sprintf("https://images.evetech.net/types/%d/bp?size=64", requests[0].TypeId)
	}
	return n
}

// blueprintRequestLine is one request in the DM, such as
// "12 runs of Dominix Blueprint (10-run copies, ME 10 / TE 20) from Skye".
func blueprintRequestLine(request db.BlueprintRequest) string {
	kind := "original"
	if request.IsCopy && request.RunsPerCopy > 0 {
		kind = commaInt(request.RunsPerCopy) + "-run copies"
	}
	runs := "runs"
	if request.RunsRequested == 1 {
		runs = "run"
	}
	return fmt.Sprintf("**%s %s** of %s (%s, ME %d / TE %d) from %s",
		commaInt(request.RunsRequested), runs, request.TypeName, kind,
		request.MaterialEfficiency, request.TimeEfficiency, request.OwnerName)
}
