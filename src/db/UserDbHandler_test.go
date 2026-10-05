package db

import (
	"testing"
	"time"
)

func TestSortMainFirst(t *testing.T) {
	start := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	characters := []UserCharacter{
		{CharacterName: "Zed", AddedAt: start.Add(time.Hour)},
		{CharacterName: "Mira", AddedAt: start},
		{CharacterName: "Alpha", AddedAt: start.Add(2 * time.Hour)},
		{CharacterName: "Bram", AddedAt: start.Add(3 * time.Hour)},
	}

	sortMainFirst(characters)

	want := []string{"Mira", "Alpha", "Bram", "Zed"}
	for i, name := range want {
		if characters[i].CharacterName != name {
			t.Fatalf("order = %v, want %v", names(characters), want)
		}
	}
}

func names(characters []UserCharacter) []string {
	out := make([]string, len(characters))
	for i, character := range characters {
		out[i] = character.CharacterName
	}
	return out
}
