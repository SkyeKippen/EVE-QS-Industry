package main

import (
	"QS-Indy/src/auth"
	"QS-Indy/src/db"
	"errors"
	"log"
	"net/http"
	"strconv"
)

// handleBlueprintSharing shows the Share Blueprints page on GET. A POST adds
// blueprints to the library (action=add) or removes them (action=remove),
// picked by level and the IDs that level needs (see db.SharingSelection),
// then goes back to the page.
func handleBlueprintSharing(w http.ResponseWriter, r *http.Request) {
	sess, _ := auth.CurrentSession(r)

	owners, err := db.SharableOwners(r.Context(), sess.UserID)
	if err != nil {
		log.Printf("blueprint sharing: owners of user %d: %v", sess.UserID, err)
		http.Error(w, "could not load your blueprints", http.StatusInternalServerError)
		return
	}

	switch r.Method {
	case http.MethodGet:
		renderBlueprintSharing(w, r, sess, owners)
	case http.MethodPost:
		if err := applyBlueprintSharing(r, owners); err != nil {
			var badForm errBadSharingForm
			if errors.As(err, &badForm) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			log.Printf("blueprint sharing: user %d: %v", sess.UserID, err)
			http.Error(w, "could not update your shared blueprints", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/blueprints/sharing", http.StatusSeeOther)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func renderBlueprintSharing(w http.ResponseWriter, r *http.Request, sess *auth.Session, owners []int64) {
	tree, err := db.LoadSharingTree(r.Context(), owners)
	if err != nil {
		log.Printf("blueprint sharing: loading tree for user %d: %v", sess.UserID, err)
		http.Error(w, "could not load your blueprints", http.StatusInternalServerError)
		return
	}

	data := struct {
		Scope string
		Tree  db.SharingTree
	}{Scope: "sharing", Tree: tree}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "blueprints_sharing.html", data); err != nil {
		log.Printf("renderBlueprintSharing: %v", err)
	}
}

// errBadSharingForm is shown for a form that didn't come from the page.
type errBadSharingForm string

func (e errBadSharingForm) Error() string { return "invalid sharing request: " + string(e) }

func applyBlueprintSharing(r *http.Request, owners []int64) error {
	var shared bool
	switch r.PostFormValue("action") {
	case "add":
		shared = true
	case "remove":
		shared = false
	default:
		return errBadSharingForm("action")
	}

	selection := db.SharingSelection{Level: r.PostFormValue("level"), Hangar: r.PostFormValue("hangar")}
	switch selection.Level {
	case db.ShareAll, db.ShareStructure, db.ShareOffice, db.ShareContainer:
	default:
		return errBadSharingForm("level")
	}
	ids := map[string]*int64{"place": &selection.PlaceId, "owner": &selection.OwnerId, "container": &selection.ContainerId}
	for field, id := range ids {
		value := r.PostFormValue(field)
		if value == "" {
			continue
		}
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return errBadSharingForm(field)
		}
		*id = parsed
	}

	changed, err := db.SetBlueprintsShared(r.Context(), owners, selection, shared)
	if err != nil {
		return err
	}
	log.Printf("blueprint sharing: %s %s %+v changed %d blueprints", r.PostFormValue("action"), selection.Level, selection, changed)
	return nil
}
