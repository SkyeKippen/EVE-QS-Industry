package main

import (
	"QS-Indy/src/db"
	"QS-Indy/src/notify"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestDueBlueprintRequestBatches(t *testing.T) {
	cutoff := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	old, recent := cutoff.Add(-time.Minute), cutoff.Add(time.Minute)
	const (
		alice, bob, carol = 1, 2, 3
		aliceChar         = 100
		bobChar           = 200
		corp              = 900 // Alice and Carol can both share its blueprints
		nobodysChar       = 300
	)
	owners := map[int64][]int64{aliceChar: {alice}, bobChar: {bob}, corp: {alice, carol}}
	waiting := []db.BlueprintRequest{
		{RequestId: 1, OwnerId: aliceChar, RequestedAt: old},
		{RequestId: 2, OwnerId: bobChar, RequestedAt: recent},
		{RequestId: 3, OwnerId: aliceChar, RequestedAt: recent},
		{RequestId: 4, OwnerId: corp, RequestedAt: recent},
		{RequestId: 5, OwnerId: nobodysChar, RequestedAt: old},
		{RequestId: 6, OwnerId: nobodysChar, RequestedAt: recent},
	}

	batches, done := dueBlueprintRequestBatches(waiting, owners, cutoff)

	// Alice's first request is 15 minutes old, so her later ones go with
	// it. Carol shares the corporation request, so she gets it now too
	// rather than missing it. Bob's only request is still collecting.
	ids := func(requests []db.BlueprintRequest) []int64 {
		var out []int64
		for _, r := range requests {
			out = append(out, r.RequestId)
		}
		return out
	}
	if got := ids(batches[alice]); !slices.Equal(got, []int64{1, 3, 4}) {
		t.Errorf("Alice's batch = %v, want [1 3 4]", got)
	}
	if got := ids(batches[carol]); !slices.Equal(got, []int64{4}) {
		t.Errorf("Carol's batch = %v, want [4]", got)
	}
	if _, ok := batches[bob]; ok {
		t.Errorf("Bob got a batch before his first request was 15 minutes old")
	}
	if !slices.Equal(done, []int64{1, 3, 4, 5}) {
		t.Errorf("done = %v, want [1 3 4 5]", done)
	}
}

func TestBlueprintRequestsNotification(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	requests := []db.BlueprintRequest{
		{RequesterCharacterName: "Pilot A", OwnerName: "Skye", TypeId: 645, TypeName: "Dominix Blueprint", IsCopy: true, RunsPerCopy: 10, MaterialEfficiency: 10, TimeEfficiency: 20, RunsRequested: 12},
		{RequesterCharacterName: "Pilot B", OwnerName: "Corp", TypeId: 645, TypeName: "Dominix Blueprint", RunsPerCopy: -1, MaterialEfficiency: 10, TimeEfficiency: 20, RunsRequested: 1},
		{RequesterCharacterName: "Pilot A", OwnerName: "Skye", TypeId: 645, TypeName: "Dominix Blueprint", IsCopy: true, RunsPerCopy: 10, MaterialEfficiency: 10, TimeEfficiency: 20, RunsRequested: 3},
	}

	n := blueprintRequestsNotification(7, requests, now)

	if n.Event != notify.EventBlueprintRequested || n.UserID != 7 {
		t.Errorf("event %q for user %d", n.Event, n.UserID)
	}
	if n.Title != "3 new blueprint requests" {
		t.Errorf("title = %q", n.Title)
	}
	want := "**Pilot A** asked for:\n" +
		"- **12 runs** of Dominix Blueprint (10-run copies, ME 10 / TE 20) from Skye\n" +
		"- **3 runs** of Dominix Blueprint (10-run copies, ME 10 / TE 20) from Skye\n" +
		"**Pilot B** asked for:\n" +
		"- **1 run** of Dominix Blueprint (original, ME 10 / TE 20) from Corp"
	if n.Description != want {
		t.Errorf("description =\n%s\nwant\n%s", n.Description, want)
	}
	if n.Fields[1].Value != "16" || n.Fields[2].Value != "2" {
		t.Errorf("fields = %+v", n.Fields)
	}
	if !strings.Contains(n.ThumbnailURL, "/types/645/") {
		t.Errorf("thumbnail = %q", n.ThumbnailURL)
	}
}

func TestBlueprintRequestsNotificationTrimsLongLists(t *testing.T) {
	requests := make([]db.BlueprintRequest, 200)
	for i := range requests {
		requests[i] = db.BlueprintRequest{RequesterCharacterName: "Pilot", OwnerName: "Skye", TypeId: i, TypeName: "Some Long Blueprint Name", RunsRequested: 1}
	}

	n := blueprintRequestsNotification(7, requests, time.Now())

	if len(n.Description) > 4096 {
		t.Errorf("description is %d bytes, over Discord's limit", len(n.Description))
	}
	if !strings.Contains(n.Description, "more") {
		t.Errorf("description doesn't say more requests were left out:\n%s", n.Description)
	}
	if n.ThumbnailURL != "" {
		t.Errorf("thumbnail set for mixed blueprints: %q", n.ThumbnailURL)
	}
}
