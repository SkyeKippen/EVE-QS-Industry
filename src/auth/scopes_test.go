package auth

import (
	"net/http/httptest"
	"slices"
	"testing"

	"QS-Indy/src/esi"
)

func TestRequestedScopes(t *testing.T) {
	cfg = &esi.Config{Scopes: []string{
		"publicData",
		"esi-universe.read_structures.v1",
		"esi-characters.read_blueprints.v1",
		"esi-corporations.read_blueprints.v1",
		"esi-assets.read_corporation_assets.v1",
	}}

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "plain login asks for every configured scope",
			query: "",
			want:  cfg.Scopes,
		},
		{
			name:  "nothing ticked keeps publicData and non-optional scopes",
			query: "?scopes_chosen=1",
			want:  []string{"publicData", "esi-universe.read_structures.v1"},
		},
		{
			name:  "only ticked optional scopes are kept",
			query: "?scopes_chosen=1&scope=esi-characters.read_blueprints.v1",
			want:  []string{"publicData", "esi-universe.read_structures.v1", "esi-characters.read_blueprints.v1"},
		},
		{
			name:  "scopes outside ESI_SCOPES are ignored",
			query: "?scopes_chosen=1&scope=esi-wallet.read_character_wallet.v1",
			want:  []string{"publicData", "esi-universe.read_structures.v1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := requestedScopes(httptest.NewRequest("GET", "/auth/login"+tt.query, nil))
			if !slices.Equal(got, tt.want) {
				t.Errorf("requestedScopes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRequestedScopesAddsPublicData(t *testing.T) {
	cfg = &esi.Config{Scopes: []string{"esi-characters.read_blueprints.v1"}}

	got := requestedScopes(httptest.NewRequest("GET", "/auth/login?scopes_chosen=1", nil))
	if !slices.Equal(got, []string{"publicData"}) {
		t.Errorf("requestedScopes() = %v, want [publicData]", got)
	}
}
