package main

import (
	"QS-Indy/src/auth"
	"QS-Indy/src/db"
	"QS-Indy/src/discord"
	"QS-Indy/src/esi"
	"QS-Indy/src/notify"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// discordConfig is nil when Discord isn't set up in config/.env, which
// turns off linking and notifications.
var discordConfig *discord.Config

// initDiscord loads the Discord settings and, when they are set, sends
// notifications as DMs from the bot.
func initDiscord() error {
	cfg, err := discord.LoadConfigFromEnv()
	if err != nil {
		return err
	}
	if cfg == nil {
		log.Println("discord: no DISCORD_* settings in config/.env, notifications are off")
		return nil
	}
	discordConfig = cfg
	notify.Init(db.NotificationRecipient, discord.Notifier{Config: cfg})
	return nil
}

const discordStateCookieName = "discord_link_state"

// discordStatusParam on /characters says how linking or a test DM went; its
// values are the keys of discordStatusMessages.
const discordStatusParam = "discord"

var discordStatusMessages = map[string]string{
	"linked":      "Discord linked. You've been added to the server, so the bot can DM you.",
	"cancelled":   "Discord linking was cancelled.",
	"failed":      "Discord linking failed. Please try again.",
	"join-failed": "Your Discord account couldn't be added to the server, so linking was stopped. Please try again.",
	"unlinked":    "Discord unlinked. You won't get any more DMs.",
	"saved":       "Notification settings saved.",
	"test-sent":   "Test message sent. Check your Discord DMs.",
	"test-failed": "The test message couldn't be sent. Please try again.",
	"dm-blocked":  "Discord refused the DM. Allow direct messages from members of the server in its Privacy Settings, then try again.",
}

func charactersWithStatus(w http.ResponseWriter, r *http.Request, status string) {
	http.Redirect(w, r, "/characters?"+discordStatusParam+"="+status, http.StatusSeeOther)
}

// discordSection is what the Characters page shows about Discord.
type discordSection struct {
	Enabled  bool // false when Discord isn't set up
	Linked   bool
	Link     db.DiscordLink
	Events   []discordEventSetting
	Status   string // a message from discordStatusMessages, or ""
	StatusOK bool
}

type discordEventSetting struct {
	notify.EventInfo
	On bool
}

func loadDiscordSection(r *http.Request, userID int64) (discordSection, error) {
	section := discordSection{Enabled: discordConfig != nil}
	status := r.URL.Query().Get(discordStatusParam)
	section.Status = discordStatusMessages[status]
	section.StatusOK = status == "linked" || status == "unlinked" || status == "saved" || status == "test-sent"
	if !section.Enabled {
		return section, nil
	}

	link, linked, err := db.LoadDiscordLink(r.Context(), userID)
	if err != nil {
		return section, err
	}
	section.Linked, section.Link = linked, link

	settings, err := db.LoadNotificationSettings(r.Context(), userID)
	if err != nil {
		return section, err
	}
	for _, info := range notify.Events() {
		section.Events = append(section.Events, discordEventSetting{info, settings[info.Event]})
	}
	return section, nil
}

// handleDiscordLink sends the user to Discord to sign in, which links their
// account and adds them to the server.
func handleDiscordLink(w http.ResponseWriter, r *http.Request) {
	if discordConfig == nil {
		http.NotFound(w, r)
		return
	}
	state, err := esi.GenerateState()
	if err != nil {
		log.Printf("discord: state generation failed: %v", err)
		charactersWithStatus(w, r, "failed")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     discordStateCookieName,
		Value:    state,
		Path:     "/discord",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(10 * time.Minute),
	})
	http.Redirect(w, r, discordConfig.AuthorizeURL(state), http.StatusFound)
}

// handleDiscordCallback finishes Discord sign-in: it adds the user to the
// server, then links their Discord account. Linking stops if they can't be
// added, since the bot can only DM people it shares a server with.
func handleDiscordCallback(w http.ResponseWriter, r *http.Request) {
	if discordConfig == nil {
		http.NotFound(w, r)
		return
	}
	sess, _ := auth.CurrentSession(r)
	q := r.URL.Query()

	stateCookie, err := r.Cookie(discordStateCookieName)
	if err != nil || q.Get("state") == "" || stateCookie.Value != q.Get("state") {
		http.Error(w, "invalid state - possible CSRF", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: discordStateCookieName, Path: "/discord", MaxAge: -1})

	// Discord sends error=access_denied when the user clicks Cancel
	if q.Get("error") != "" || q.Get("code") == "" {
		charactersWithStatus(w, r, "cancelled")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	accessToken, err := discordConfig.ExchangeCode(ctx, q.Get("code"))
	if err != nil {
		log.Printf("discord: user %d: %v", sess.UserID, err)
		charactersWithStatus(w, r, "failed")
		return
	}
	user, err := discord.CurrentUser(ctx, accessToken)
	if err != nil {
		log.Printf("discord: user %d: %v", sess.UserID, err)
		charactersWithStatus(w, r, "failed")
		return
	}
	if err := discordConfig.JoinGuild(ctx, user.ID, accessToken); err != nil {
		log.Printf("discord: user %d: %v", sess.UserID, err)
		charactersWithStatus(w, r, "join-failed")
		return
	}
	if err := db.SaveDiscordLink(ctx, sess.UserID, user.ID, user.DisplayName()); err != nil {
		log.Printf("discord: saving link of user %d to %s: %v", sess.UserID, user.ID, err)
		charactersWithStatus(w, r, "failed")
		return
	}

	log.Printf("discord: user %d linked Discord %s (%s)", sess.UserID, user.ID, user.Username)
	charactersWithStatus(w, r, "linked")
}

func handleDiscordUnlink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess, _ := auth.CurrentSession(r)
	if err := db.DeleteDiscordLink(r.Context(), sess.UserID); err != nil {
		log.Printf("discord: unlinking user %d: %v", sess.UserID, err)
		http.Error(w, "failed to unlink Discord", http.StatusInternalServerError)
		return
	}
	charactersWithStatus(w, r, "unlinked")
}

// handleDiscordSettings saves which notifications the user wants. A ticked
// switch posts event=<event>.
func handleDiscordSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	sess, _ := auth.CurrentSession(r)

	on := map[string]bool{}
	for _, event := range r.PostForm["event"] {
		on[event] = true
	}
	settings := map[notify.Event]bool{}
	for _, info := range notify.Events() {
		settings[info.Event] = on[string(info.Event)]
	}
	if err := db.SaveNotificationSettings(r.Context(), sess.UserID, settings); err != nil {
		log.Printf("discord: saving notification settings of user %d: %v", sess.UserID, err)
		http.Error(w, "failed to save notification settings", http.StatusInternalServerError)
		return
	}
	charactersWithStatus(w, r, "saved")
}

// handleDiscordTest DMs the user a sample notification so they can check
// the bot reaches them.
func handleDiscordTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if discordConfig == nil {
		http.NotFound(w, r)
		return
	}
	sess, _ := auth.CurrentSession(r)

	link, linked, err := db.LoadDiscordLink(r.Context(), sess.UserID)
	if err != nil || !linked {
		if err != nil {
			log.Printf("discord: test DM for user %d: %v", sess.UserID, err)
		}
		charactersWithStatus(w, r, "test-failed")
		return
	}

	err = notify.SendNow(r.Context(), notify.Recipient{DiscordUserID: link.DiscordUserID}, notify.Notification{
		UserID:      sess.UserID,
		Title:       "Test notification",
		URL:         siteURL("/characters"),
		Description: fmt.Sprintf("Hi %s, notifications from %s will arrive here.", sess.CharacterName, siteName),
		Color:       notify.ColorInfo,
		Footer:      siteName,
		Timestamp:   time.Now(),
	})
	var apiErr *discord.APIError
	switch {
	case errors.As(err, &apiErr) && apiErr.Code == discord.CodeCannotDMUser:
		charactersWithStatus(w, r, "dm-blocked")
	case err != nil:
		log.Printf("discord: test DM for user %d: %v", sess.UserID, err)
		charactersWithStatus(w, r, "test-failed")
	default:
		charactersWithStatus(w, r, "test-sent")
	}
}

const siteName = "QS Industry Project"

// siteURL is path on the public site, which is wherever EVE SSO sends
// people back to. It is "" when that isn't known, which leaves links out.
func siteURL(path string) string {
	u, err := url.Parse(os.Getenv("ESI_CALLBACK_URL"))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host + path
}

// notifyOrderFulfilled tells an order's owner that fulfilledBy marked it
// fulfilled and it is waiting for them to confirm it.
func notifyOrderFulfilled(orderId int, fulfilledBy string) {
	if !notify.Enabled() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	order, err := db.FetchOrderById(orderId)
	if err != nil {
		log.Printf("notify: loading fulfilled order %d: %v", orderId, err)
		return
	}
	ownerID, ok, err := db.UserIDForCharacterName(ctx, order.CreatedBy)
	if err != nil || !ok {
		if err != nil {
			log.Printf("notify: finding owner %q of order %d: %v", order.CreatedBy, orderId, err)
		}
		return
	}
	notify.Send(orderFulfilledNotification(order, ownerID, fulfilledBy))
}

func orderFulfilledNotification(order db.Order, ownerID int64, fulfilledBy string) notify.Notification {
	side := "Sell"
	if order.IsBuyOrder {
		side = "Buy"
	}
	contractTo := order.ContractTo
	if contractTo == "" {
		contractTo = "Not set"
	}
	return notify.Notification{
		Event:  notify.EventOrderFulfilled,
		UserID: ownerID,
		Title:  fmt.Sprintf("Order #%d fulfilled: %s x %s", order.InternalIdCounter, commaInt(order.Quantity), order.TypeName),
		URL:    siteURL("/user-orders"),
		Description: fmt.Sprintf("**%s** marked your %s order for **%s** fulfilled. Check what you received, then confirm or deny it on My Orders.",
			fulfilledBy, strings.ToLower(side), order.TypeName),
		Color:        notify.ColorPending,
		ThumbnailURL: fmt.Sprintf("https://images.evetech.net/types/%d/icon?size=64", order.TypeId),
		Fields: []notify.Field{
			{Name: "Item", Value: order.TypeName, Inline: true},
			{Name: "Order type", Value: side, Inline: true},
			{Name: "Quantity", Value: commaInt(order.Quantity), Inline: true},
			{Name: "Price per unit", Value: commaPrice(order.PricePerUnit()) + " ISK", Inline: true},
			{Name: "Total price", Value: commaPrice(order.Price) + " ISK", Inline: true},
			{Name: "Location", Value: order.Location, Inline: true},
			{Name: "Contract to", Value: contractTo, Inline: true},
			{Name: "Fulfilled by", Value: fulfilledBy, Inline: true},
			{Name: "Order ID", Value: "#" + strconv.Itoa(order.InternalIdCounter), Inline: true},
		},
		Footer:    siteName,
		Timestamp: time.Now(),
	}
}
