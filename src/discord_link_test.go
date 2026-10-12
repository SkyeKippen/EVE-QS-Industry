package main

import (
	"QS-Indy/src/db"
	"QS-Indy/src/notify"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOrderFulfilledNotification(t *testing.T) {
	t.Setenv("ESI_CALLBACK_URL", "https://indy.example.com/auth/callback")
	order := db.Order{
		InternalIdCounter: 42,
		TypeId:            34,
		TypeName:          "Tritanium",
		IsBuyOrder:        true,
		Quantity:          1500000,
		Price:             6000000,
		Location:          "Jita IV - Moon 4",
		CreatedBy:         "Owner Pilot",
	}
	n := orderFulfilledNotification(order, 7, "Hauler Pilot")

	if n.Event != notify.EventOrderFulfilled || n.UserID != 7 {
		t.Errorf("event %q to user %d", n.Event, n.UserID)
	}
	if n.URL != "https://indy.example.com/user-orders" {
		t.Errorf("URL = %q", n.URL)
	}
	if n.Title != "Order #42 fulfilled: 1,500,000 x Tritanium" {
		t.Errorf("title = %q", n.Title)
	}
	if !strings.Contains(n.Description, "**Hauler Pilot** marked your buy order") {
		t.Errorf("description = %q", n.Description)
	}
	want := map[string]string{
		"Item":           "Tritanium",
		"Order type":     "Buy",
		"Quantity":       "1,500,000",
		"Price per unit": "4 ISK",
		"Total price":    "6,000,000 ISK",
		"Location":       "Jita IV - Moon 4",
		"Contract to":    "Not set",
		"Fulfilled by":   "Hauler Pilot",
		"Order ID":       "#42",
	}
	for _, f := range n.Fields {
		if want[f.Name] != f.Value {
			t.Errorf("field %q = %q, want %q", f.Name, f.Value, want[f.Name])
		}
		delete(want, f.Name)
	}
	for name := range want {
		t.Errorf("missing field %q", name)
	}
}

func TestCharactersPageDiscordSection(t *testing.T) {
	render := func(section discordSection) string {
		data := struct {
			CharacterID int64
			Characters  []db.UserCharacter
			Discord     discordSection
		}{Discord: section}
		w := httptest.NewRecorder()
		if err := tmpl.ExecuteTemplate(w, "characters.html", data); err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(w.Body)
		return string(body)
	}

	if page := render(discordSection{}); strings.Contains(page, "Discord") {
		t.Errorf("the Discord section should be hidden when Discord isn't set up")
	}
	if page := render(discordSection{Enabled: true}); !strings.Contains(page, `href="/discord/link"`) {
		t.Errorf("an unlinked user should see Link Discord")
	}

	linked := render(discordSection{
		Enabled: true,
		Linked:  true,
		Link:    db.DiscordLink{DiscordName: "skye"},
		Events:  []discordEventSetting{{notify.Events()[0], false}},
		Status:  discordStatusMessages["saved"],
	})
	for _, want := range []string{"Linked to <strong>skye</strong>", `value="order_fulfilled">`, "/discord/unlink", `formaction="/discord/test"`, "Notification settings saved."} {
		if !strings.Contains(linked, want) {
			t.Errorf("linked page is missing %q", want)
		}
	}
}
