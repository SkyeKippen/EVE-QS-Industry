package auth

import (
	"net/http"
	"slices"
)

// RequiredScope is always requested at sign-in, whatever the user picks.
const RequiredScope = "publicData"

// ScopeChoice is a scope the user may leave out at sign-in.
type ScopeChoice struct {
	Scope       string
	Label       string
	Description string
}

var optionalScopes = []ScopeChoice{
	{
		Scope:       "esi-characters.read_blueprints.v1",
		Label:       "Character blueprints",
		Description: "List your character's blueprints in the Blueprint Library.",
	},
	{
		Scope:       "esi-corporations.read_blueprints.v1",
		Label:       "Corporation blueprints",
		Description: "List your corporation's blueprints (needs the Director role in game).",
	},
	{
		Scope:       "esi-assets.read_corporation_assets.v1",
		Label:       "Corporation assets",
		Description: "Find which container or office a corporation blueprint is in (needs the Director role in game).",
	},
}

// chosenParam marks a sign-in that came from the scope picker, so an empty
// selection means "none of the optional scopes" rather than "not asked".
const chosenParam = "scopes_chosen"

// ScopeChoices returns the optional scopes that ESI_SCOPES requests, for the
// sign-in form.
func ScopeChoices() []ScopeChoice {
	var choices []ScopeChoice
	for _, choice := range optionalScopes {
		if slices.Contains(cfg.Scopes, choice.Scope) {
			choices = append(choices, choice)
		}
	}
	return choices
}

// requestedScopes returns the scopes to send to EVE SSO for this sign-in.
// Optional scopes are dropped when the user unticked them on the sign-in form;
// a plain /auth/login asks for every configured scope. RequiredScope is always
// included, and nothing outside ESI_SCOPES can be added from the request.
func requestedScopes(r *http.Request) []string {
	q := r.URL.Query()
	chosen := q.Has(chosenParam)
	picked := q["scope"]

	scopes := []string{RequiredScope}
	for _, scope := range cfg.Scopes {
		if scope == RequiredScope {
			continue
		}
		if chosen && isOptional(scope) && !slices.Contains(picked, scope) {
			continue
		}
		scopes = append(scopes, scope)
	}
	return scopes
}

func isOptional(scope string) bool {
	return slices.ContainsFunc(optionalScopes, func(choice ScopeChoice) bool {
		return choice.Scope == scope
	})
}
