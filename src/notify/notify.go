// Package notify sends a user a message when something they care about
// happens, such as one of their orders being fulfilled. It knows nothing
// about where messages go: a Sender delivers them (Discord DMs today) and a
// RecipientLookup decides who gets them, so new events only need a new Event
// and a Notification built where the event happens.
package notify

import (
	"context"
	"errors"
	"log"
	"time"
)

// Event names a kind of notification. It is stored with each user's
// notification settings, so existing values must not change.
type Event string

const (
	EventOrderFulfilled     Event = "order_fulfilled"
	EventBlueprintRequested Event = "blueprint_requested"
)

// EventInfo describes an event on the settings page.
type EventInfo struct {
	Event       Event
	Label       string
	Description string
}

// Events lists every event a user can turn on or off, in the order the
// settings page shows them.
func Events() []EventInfo {
	return []EventInfo{
		{EventOrderFulfilled, "Order fulfilled", "When someone marks one of your orders fulfilled and it is waiting for you to confirm it."},
		{EventBlueprintRequested, "Blueprint requested", "When someone requests runs of blueprints you shared. Requests are collected for 15 minutes after the first one, then sent together."},
	}
}

// Colors for Notification.Color, as 0xRRGGBB.
const (
	ColorGood    = 0x16a34a
	ColorPending = 0xd97706
	ColorInfo    = 0x2563eb
)

// Field is one labelled value in a notification, shown side by side with
// other inline fields.
type Field struct {
	Name   string
	Value  string
	Inline bool
}

// Notification is one message to one user of the app.
type Notification struct {
	Event  Event
	UserID int64 // the app user to tell, not a character

	Title        string
	URL          string // the title links here when set
	Description  string
	Color        int
	ThumbnailURL string
	Fields       []Field
	Footer       string
	Timestamp    time.Time
}

// Recipient is where a user's notifications are delivered.
type Recipient struct {
	DiscordUserID string
}

// RecipientLookup returns where userID wants event delivered, or ok false
// when they have nowhere linked or have turned the event off.
type RecipientLookup func(ctx context.Context, userID int64, event Event) (recipient Recipient, ok bool, err error)

// Sender delivers a notification to a recipient.
type Sender interface {
	Send(ctx context.Context, recipient Recipient, n Notification) error
}

// ErrDisabled is returned when no Sender is set up.
var ErrDisabled = errors.New("notifications are not set up")

var (
	lookup RecipientLookup
	sender Sender
)

// Init sets where notifications go. Until it is called, or when either
// argument is nil, Send drops every notification.
func Init(recipientLookup RecipientLookup, s Sender) {
	lookup = recipientLookup
	sender = s
}

// Enabled reports whether notifications can be delivered at all.
func Enabled() bool {
	return lookup != nil && sender != nil
}

// sendTimeout bounds one delivery, including the lookup.
const sendTimeout = 30 * time.Second

// Send delivers n in the background, so a slow or failing delivery never
// holds up the request that caused it. Failures are logged.
func Send(n Notification) {
	if !Enabled() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		defer cancel()
		if err := deliver(ctx, n); err != nil {
			log.Printf("notify: %s to user %d: %v", n.Event, n.UserID, err)
		}
	}()
}

// SendNow delivers n and waits, ignoring the user's settings for the event.
// It is for test messages the user asked for, so they see any error.
func SendNow(ctx context.Context, recipient Recipient, n Notification) error {
	if sender == nil {
		return ErrDisabled
	}
	return sender.Send(ctx, recipient, n)
}

func deliver(ctx context.Context, n Notification) error {
	recipient, ok, err := lookup(ctx, n.UserID, n.Event)
	if err != nil || !ok {
		return err
	}
	if n.Timestamp.IsZero() {
		n.Timestamp = time.Now()
	}
	return sender.Send(ctx, recipient, n)
}
