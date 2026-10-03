package main

import (
	"QS-Indy/src/auth"
	"QS-Indy/src/db"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

func commaInt(n int) string {
	s := fmt.Sprintf("%d", n)
	return addCommas(s)
}

func commaFloat(n float64) string {
	s := fmt.Sprintf("%.2f", n) // 2 decimal places for money
	parts := strings.SplitN(s, ".", 2)
	parts[0] = addCommas(parts[0])
	return strings.Join(parts, ".")
}

// commaPrice formats an ISK amount for an input field with thousands
// separators, no exponent, and cents only when there are any,
// e.g. 8138285000 -> "8,138,285,000" and 4.5 -> "4.50".
func commaPrice(n float64) string {
	return strings.TrimSuffix(commaFloat(n), ".00")
}

func addCommas(s string) string {
	neg := false
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	}

	var result []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, c)
	}

	out := string(result)
	if neg {
		out = "-" + out
	}
	return out
}

var tmpl = template.Must(
	template.New("").Funcs(template.FuncMap{
		"commaInt":   commaInt,
		"commaFloat": commaFloat,
		"commaPrice": commaPrice,
	}).ParseGlob("src/templates/*.html"),
)

func handleLogout(w http.ResponseWriter, r *http.Request) {
	auth.ClearSessionCookie(w, r)
	http.Redirect(w, r, "/", http.StatusFound)
}

func main() {
	err := godotenv.Load("config/.env")
	if err != nil {
		log.Fatal("Error loading .env file: ", err)
	}

	if err := auth.InitAuth(); err != nil {
		log.Fatalf("evesso: config: %v", err)
	}

	_, thisFile, _, _ := runtime.Caller(0)
	baseDir := filepath.Dir(thisFile)
	staticDir := filepath.Join(baseDir, "static")

	fs := http.FileServer(http.Dir(staticDir))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	http.HandleFunc("/", renderBase)
	http.HandleFunc("/blueprints", auth.RequireAuth(renderBlueprints))
	http.HandleFunc("/order-board", auth.RequireAuth(renderOrderBoard))

	http.HandleFunc("/create-order", auth.RequireAuth(renderCreateOrder))
	http.HandleFunc("/create-order/item-suggestions", auth.RequireAuth(handleItemSuggestions))
	http.HandleFunc("/process-order-creation", auth.RequireAuth(renderOrderCreation))

	http.HandleFunc("/fulfill-order", auth.RequireAuth(renderFulfillOrder))

	http.HandleFunc("/fulfill-order/confirm", auth.RequireAuth(handleFulfillOrderConfirm))

	http.HandleFunc("/order-board/claim", auth.RequireAuth(handleClaimOrder))
	http.HandleFunc("/order-board/unclaim", auth.RequireAuth(handleUnclaimOrder))
	http.HandleFunc("/user-orders/force-unclaim", auth.RequireAuth(handleForceUnclaimOrder))

	http.HandleFunc("/user-orders", auth.RequireAuth(renderUserOrders))
	http.HandleFunc("/manage-order", auth.RequireAuth(renderManageOrder))
	http.HandleFunc("/manage-order/submit-changes", auth.RequireAuth(renderSubmitOrderChanges))
	http.HandleFunc("/manage-order/delete-order", auth.RequireAuth(renderDeleteOrder))

	http.HandleFunc("/user-orders/verify-fulfillment", auth.RequireAuth(handleVerifyFulfillment))
	http.HandleFunc("/user-orders/deny-fulfillment", auth.RequireAuth(handleDenyFulfillment))

	http.HandleFunc("/auth/login", auth.HandleLogin)
	http.HandleFunc("/auth/callback", auth.HandleCallback)
	http.HandleFunc("/auth/logout", handleLogout)

	log.Println("Server running at https://qsindy.skyemeadows.net (port 5001 in production)")
	err = http.ListenAndServe(":5001", nil)
	if err != nil {
		return
	}
}

func renderBase(w http.ResponseWriter, r *http.Request) {
	sess, loggedIn := auth.CurrentSession(r)

	log.Printf("renderBase: loggedIn=%v session=%+v", loggedIn, sess)

	data := struct {
		LoggedIn      bool
		CharacterName string
	}{LoggedIn: loggedIn}
	if loggedIn {
		data.CharacterName = sess.CharacterName
		data.LoggedIn = true
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err := tmpl.ExecuteTemplate(w, "base.html", data)
	if err != nil {
		return
	}
}

func renderBlueprints(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err := tmpl.ExecuteTemplate(w, "blueprints_browser.html", nil)
	if err != nil {
		return
	}
}

func renderOrderBoard(w http.ResponseWriter, r *http.Request) {
	sess, _ := auth.CurrentSession(r)

	start := time.Now()
	orders, err := db.LoadAllIndustryOrders()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Println("Time to process LoadAllIndustryOrders:", time.Since(start))

	// CharacterName decides which claim buttons each row shows.
	data := struct {
		CharacterName string
		Orders        []db.Order
	}{CharacterName: sess.CharacterName, Orders: orders}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = tmpl.ExecuteTemplate(w, "order_board.html", data)
	if err != nil {
		return
	}
}

func renderCreateOrder(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err := tmpl.ExecuteTemplate(w, "create_order.html", nil)
	if err != nil {
		return
	}
}

// handleItemSuggestions returns, as a JSON array, up to 5 item names
// matching the q query parameter, for the Create Order item field.
func handleItemSuggestions(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	names := []string{}
	if len([]rune(strings.TrimSpace(query))) >= 3 {
		var err error
		names, err = db.SuggestItemNames(query, 5)
		if err != nil {
			log.Println("Error suggesting item names:", err)
			http.Error(w, "item lookup failed", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(names); err != nil {
		log.Println("Error writing item suggestions:", err)
	}
}

func renderOrderCreation(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		log.Println("Error Parsing Form:", err)
		return
	}

	sess, _ := auth.CurrentSession(r)

	orderItem := strings.TrimSpace(r.PostFormValue("order-item"))
	orderQuantity, err := strconv.ParseInt(r.PostFormValue("order-quantity"), 10, 64)
	if err != nil {
		http.Error(w, "quantity must be a whole number", http.StatusBadRequest)
		return
	}
	orderPrice, err := parsePrice(r.PostFormValue("order-price"))
	if err != nil {
		http.Error(w, err.Error()+" (e.g. 40.34M, 1.5k, 8.5B or 8,138,285,000)", http.StatusBadRequest)
		return
	}
	orderLocation := strings.TrimSpace(r.PostFormValue("order-location"))
	orderContractTo := strings.TrimSpace(r.PostFormValue("order-contract-to"))

	if len(orderItem) > 50 {
		http.Error(w, "Item Name field exceeds maximum length", http.StatusBadRequest)
		log.Println("Error Parsing Form (OrderItem):", err)
		return
	}

	if len(orderLocation) > 50 {
		http.Error(w, "Location field exceeds maximum length", http.StatusBadRequest)
		log.Println("Error Parsing Form (OrderLocation):", err)
		return
	}

	if len(orderContractTo) > 50 {
		http.Error(w, "Contract To field exceeds maximum length", http.StatusBadRequest)
		log.Println("Error Parsing Form (ContractTo):", err)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		log.Println("Unknown Error Parsing Form:", err)
		return
	}

	orderId, err := db.ProcessOrderCreation(orderItem, orderQuantity, orderPrice, orderLocation, orderContractTo, sess.CharacterName)
	if err != nil {
		log.Println("Handling error in order creation process:", err)
		data := struct {
			InvalidTypeError bool
		}{InvalidTypeError: true}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		err = tmpl.ExecuteTemplate(w, "create_order.html", data)
		return
	}

	log.Println("Created order ID", orderId, "with the values:", orderItem, orderQuantity, orderPrice, orderLocation, orderContractTo, "by", sess.CharacterName)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = tmpl.ExecuteTemplate(w, "create_order.html", nil)
	if err != nil {
		return
	}
}

func renderFulfillOrder(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Error(w, "Missing InternalIdCounter parameter", http.StatusBadRequest)
		return
	}

	orderId, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	order, err := db.FetchOrderById(orderId)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sess, _ := auth.CurrentSession(r)
	if order.ClaimedBy != "" && order.ClaimedBy != sess.CharacterName {
		http.Error(w, db.ErrOrderClaimed.Error(), http.StatusConflict)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = tmpl.ExecuteTemplate(w, "fulfill_order.html", order)
	if err != nil {
		return
	}
}

func handleFulfillOrderConfirm(w http.ResponseWriter, r *http.Request) {
	idStr := r.PostFormValue("id")
	orderId, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sess, _ := auth.CurrentSession(r)

	err = db.MarkOrderFulfilled(orderId, sess.CharacterName)
	if errors.Is(err, db.ErrOrderClaimed) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Println("Order Id", idStr, "marked as fulfilled by", sess.CharacterName)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.Redirect(w, r, "/order-board", http.StatusSeeOther)
}

func renderUserOrders(w http.ResponseWriter, r *http.Request) {
	sess, loggedIn := auth.CurrentSession(r)

	orders, err := db.LoadUserOrders(sess)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	userFulfilledOrders, err := db.LoadUserFulfilledOrders(sess)

	// Confirmed orders get their own collapsible table below the pending ones.
	var activeOrders, completedOrders []db.Order
	for _, order := range orders {
		if order.Completed {
			completedOrders = append(completedOrders, order)
		} else {
			activeOrders = append(activeOrders, order)
		}
	}

	data := struct {
		LoggedIn        bool
		CharacterName   string
		Orders          []db.Order
		ReadyOrders     []db.Order
		CompletedOrders []db.Order
	}{LoggedIn: loggedIn, CharacterName: sess.CharacterName, Orders: activeOrders, ReadyOrders: userFulfilledOrders, CompletedOrders: completedOrders}
	if loggedIn {
		data.CharacterName = sess.CharacterName
		data.LoggedIn = true
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = tmpl.ExecuteTemplate(w, "user_orders.html", data)
	if err != nil {
		return
	}
}

func renderManageOrder(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Error(w, "Missing InternalIdCounter parameter", http.StatusBadRequest)
		return
	}

	orderId, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	order, err := db.FetchOrderById(orderId)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = tmpl.ExecuteTemplate(w, "manage_order.html", order)
	if err != nil {
		return
	}
}

const maxOrderTextLen = 50

func renderSubmitOrderChanges(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sess, _ := auth.CurrentSession(r)

	orderInternalId, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue("order-id")))
	if err != nil || orderInternalId <= 0 {
		http.Error(w, "invalid order id", http.StatusBadRequest)
		return
	}

	orderQuantity, err := strconv.ParseInt(strings.TrimSpace(r.PostFormValue("order-quantity")), 10, 64)
	if err != nil || orderQuantity <= 0 {
		http.Error(w, "quantity must be a whole number greater than zero", http.StatusBadRequest)
		return
	}

	// The form pre-fills price with thousands separators (e.g. "8,138,285,000");
	// parsePrice also accepts k/M/B shorthand such as "40.34M".
	orderPrice, err := parsePrice(r.PostFormValue("order-price"))
	if err != nil {
		http.Error(w, err.Error()+" (e.g. 40.34M, 1.5k, 8.5B or 8,138,285,000)", http.StatusBadRequest)
		return
	}

	orderLocation := strings.TrimSpace(r.PostFormValue("order-location"))
	if orderLocation == "" || len(orderLocation) > maxOrderTextLen {
		http.Error(w, fmt.Sprintf("location is required and must be at most %d characters", maxOrderTextLen), http.StatusBadRequest)
		return
	}

	orderContractTo := strings.TrimSpace(r.PostFormValue("order-contract-to"))
	if orderContractTo == "" || len(orderContractTo) > maxOrderTextLen {
		http.Error(w, fmt.Sprintf("contract to is required and must be at most %d characters", maxOrderTextLen), http.StatusBadRequest)
		return
	}

	log.Println("Got values from forums as:", orderQuantity, orderPrice, orderLocation, orderContractTo, "by", sess.CharacterName)

	err = db.ProcessOrderModification(sess, orderInternalId, orderQuantity, orderPrice, orderLocation, orderContractTo)
	if errors.Is(err, db.ErrOrderNotEditable) {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.Redirect(w, r, "/user-orders", http.StatusSeeOther)
}

func renderDeleteOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sess, _ := auth.CurrentSession(r)

	orderInternalId, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue("order-id")))
	if err != nil || orderInternalId <= 0 {
		http.Error(w, "invalid order id", http.StatusBadRequest)
		return
	}

	log.Println("DELETING ORDER", orderInternalId, "REQUESTED BY", sess.CharacterName)

	err = db.DeleteOrder(orderInternalId, sess.CharacterName)
	if errors.Is(err, db.ErrOrderNotEditable) {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/user-orders", http.StatusSeeOther)
}

func handleVerifyFulfillment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.PostFormValue("order-id")
	orderId, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sess, _ := auth.CurrentSession(r)

	err = db.VerifyFulfilledOrder(sess, orderId)
	if errors.Is(err, db.ErrNotAwaitingVerification) {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Println("Order Id", idStr, "fulfillment verified by", sess.CharacterName)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.Redirect(w, r, "/user-orders", http.StatusSeeOther)
}

func handleDenyFulfillment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.PostFormValue("order-id")
	orderId, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sess, _ := auth.CurrentSession(r)

	err = db.DenyFulfilledOrder(sess, orderId)
	if errors.Is(err, db.ErrNotAwaitingVerification) {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Println("Order Id", idStr, "fulfillment denied by", sess.CharacterName)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.Redirect(w, r, "/user-orders", http.StatusSeeOther)
}

// handleClaimChange parses the posted order-id, applies change for the
// logged-in character and redirects to back. Claim rules are enforced by the
// db layer, so a forged or stale request gets a 409 rather than taking effect.
func handleClaimChange(w http.ResponseWriter, r *http.Request, action, back string, change func(orderId int, characterName string) error) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	orderId, err := strconv.Atoi(r.PostFormValue("order-id"))
	if err != nil {
		http.Error(w, "invalid order id", http.StatusBadRequest)
		return
	}

	sess, _ := auth.CurrentSession(r)

	err = change(orderId, sess.CharacterName)
	if errors.Is(err, db.ErrClaimNotAllowed) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Println("Order Id", orderId, action, "by", sess.CharacterName)

	http.Redirect(w, r, back, http.StatusSeeOther)
}

func handleClaimOrder(w http.ResponseWriter, r *http.Request) {
	handleClaimChange(w, r, "claimed", "/order-board", db.ClaimOrder)
}

func handleUnclaimOrder(w http.ResponseWriter, r *http.Request) {
	handleClaimChange(w, r, "unclaimed", "/order-board", db.UnclaimOrder)
}

func handleForceUnclaimOrder(w http.ResponseWriter, r *http.Request) {
	handleClaimChange(w, r, "force-unclaimed", "/user-orders", db.ForceUnclaimOrder)
}
