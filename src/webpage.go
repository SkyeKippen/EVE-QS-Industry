package main

import (
	"QS-Indy/src/auth"
	"QS-Indy/src/db"
	"QS-Indy/src/esi"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
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

// saveTokenAndRefreshBlueprints saves a character's tokens after sign-in,
// then refreshes their (and their corporation's) blueprints in the
// background so the Blueprint Library is up to date without slowing sign-in.
func saveTokenAndRefreshBlueprints(ctx context.Context, token esi.CharacterToken) error {
	if err := db.SaveCharacterToken(ctx, token); err != nil {
		return err
	}
	db.RefreshBlueprintsInBackground(auth.Config(), db.TokenCharacter{
		CharacterID:   token.CharacterID,
		CharacterName: token.CharacterName,
		Scopes:        token.Scopes,
	})
	return nil
}

func main() {
	err := godotenv.Load("config/.env")
	if err != nil {
		log.Fatal("Error loading .env file: ", err)
	}

	if err := auth.InitAuth(saveTokenAndRefreshBlueprints, db.LinkCharacter); err != nil {
		log.Fatalf("evesso: config: %v", err)
	}

	_, thisFile, _, _ := runtime.Caller(0)
	baseDir := filepath.Dir(thisFile)
	staticDir := filepath.Join(baseDir, "static")

	fs := http.FileServer(http.Dir(staticDir))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	http.HandleFunc("/", renderBase)
	http.HandleFunc("/blueprints", auth.RequireAuth(renderBlueprints))
	http.HandleFunc("/blueprint-icon", auth.RequireAuth(handleBlueprintIcon))
	http.HandleFunc("/order-board", auth.RequireAuth(renderOrderBoard))

	http.HandleFunc("/characters", auth.RequireAuth(renderCharacters))
	http.HandleFunc("/characters/add", auth.RequireAuth(renderAddAltCharacter))
	http.HandleFunc("/characters/deauthorize", auth.RequireAuth(handleDeauthorizeCharacter))

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
	http.HandleFunc("/auth/deauthorize-all", auth.RequireAuth(handleDeauthorizeAll))

	port := os.Getenv("SERVER_PORT")
	if port == "" {
		port = "5001"
	}

	log.Printf("Server listening on port %s", port)
	err = http.ListenAndServe(":"+port, nil)
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
		AddAlt        bool
		RequiredScope string
		ScopeChoices  []auth.ScopeChoice
	}{LoggedIn: loggedIn, RequiredScope: auth.RequiredScope, ScopeChoices: auth.ScopeChoices()}
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

// Blueprint Library scopes, chosen with ?scope= on /blueprints.
const (
	blueprintScopeMine = "mine" // the signed-in character's blueprints
	blueprintScopeCorp = "corp" // blueprints owned by the character's corporation
	blueprintScopeAll  = "all"  // every blueprint the app knows about
)

func renderBlueprints(w http.ResponseWriter, r *http.Request) {
	sess, _ := auth.CurrentSession(r)

	// ?scope=corp or ?scope=all pick those sets; anything else shows the character's own.
	scope := blueprintScopeMine
	switch r.URL.Query().Get("scope") {
	case blueprintScopeCorp:
		scope = blueprintScopeCorp
	case blueprintScopeAll:
		scope = blueprintScopeAll
	}

	// ownerId 0 means every owner.
	var ownerId int64
	switch scope {
	case blueprintScopeMine:
		ownerId = sess.CharacterID
	case blueprintScopeCorp:
		corporationId, err := esi.GetCharacterCorporationCached(int(sess.CharacterID))
		if err != nil {
			log.Printf("renderBlueprints: corporation of character %d: %v", sess.CharacterID, err)
			http.Error(w, "could not look up your corporation", http.StatusBadGateway)
			return
		}
		ownerId = corporationId
	}
	blueprints, err := db.LoadBlueprintLibrary(ownerId, scope == blueprintScopeCorp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Scope tells the page (and anything added to it later) which set is shown.
	data := struct {
		CharacterID   int64
		CharacterName string
		Scope         string
		Blueprints    []db.BlueprintLibraryRow
	}{CharacterID: sess.CharacterID, CharacterName: sess.CharacterName, Scope: scope, Blueprints: blueprints}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = tmpl.ExecuteTemplate(w, "blueprints_library.html", data)
	if err != nil {
		return
	}
}

// handleBlueprintIcon serves a blueprint's icon from the local cache,
// downloading it from the EVE image server the first time.
// ?type=<typeID>, plus &copy=1 for the blueprint copy icon.
func handleBlueprintIcon(w http.ResponseWriter, r *http.Request) {
	typeId, err := strconv.Atoi(r.URL.Query().Get("type"))
	if err != nil || typeId <= 0 {
		http.Error(w, "invalid type", http.StatusBadRequest)
		return
	}
	isCopy := r.URL.Query().Get("copy") == "1"

	path, err := esi.BlueprintIconPath(typeId, isCopy)
	if errors.Is(err, esi.ErrIconNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("blueprint icon %d: %v", typeId, err)
		http.Error(w, "could not load icon", http.StatusBadGateway)
		return
	}

	// icons never change, so let the browser keep them for a week
	w.Header().Set("Cache-Control", "private, max-age=604800")
	http.ServeFile(w, r, path)
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

	// ?side=sell shows sell orders; anything else shows buy orders.
	side := "buy"
	if r.URL.Query().Get("side") == "sell" {
		side = "sell"
	}
	sideOrders := []db.Order{}
	for _, order := range orders {
		if order.IsBuyOrder == (side == "buy") {
			sideOrders = append(sideOrders, order)
		}
	}

	// CharacterName decides which claim buttons each row shows.
	data := struct {
		CharacterName string
		Side          string
		Orders        []db.Order
	}{CharacterName: sess.CharacterName, Side: side, Orders: sideOrders}

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
	var orderIsBuyOrder bool
	switch r.PostFormValue("order-side") {
	case "buy":
		orderIsBuyOrder = true
	case "sell":
		orderIsBuyOrder = false
	default:
		http.Error(w, "choose Buy or Sell", http.StatusBadRequest)
		return
	}
	orderQuantity, err := parseQuantity(r.PostFormValue("order-quantity"))
	if err != nil {
		http.Error(w, err.Error()+" (e.g. 100, 1,200 or 1.2k)", http.StatusBadRequest)
		return
	}
	orderPrice, err := orderTotalFromForm(r, orderQuantity)
	if err != nil {
		http.Error(w, err.Error()+" (e.g. 40.34M, 1.5k, 8.5B or 8,138,285,000)", http.StatusBadRequest)
		return
	}
	orderLocation := strings.TrimSpace(r.PostFormValue("order-location"))
	// Sell orders have no Contract To; the form hides it.
	orderContractTo := ""
	if orderIsBuyOrder {
		orderContractTo = strings.TrimSpace(r.PostFormValue("order-contract-to"))
	}

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

	orderId, err := db.ProcessOrderCreation(orderItem, orderIsBuyOrder, orderQuantity, orderPrice, orderLocation, orderContractTo, sess.CharacterName)
	if err != nil {
		log.Println("Handling error in order creation process:", err)
		suggestions, sugErr := db.SuggestItemNames(orderItem, 5)
		if sugErr != nil {
			log.Println("Error suggesting item names:", sugErr)
		}
		data := struct {
			InvalidTypeError bool
			ItemSuggestions  []string
		}{InvalidTypeError: true, ItemSuggestions: suggestions}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		err = tmpl.ExecuteTemplate(w, "create_order.html", data)
		return
	}

	log.Println("Created order ID", orderId, "with the values:", orderItem, "buy:", orderIsBuyOrder, orderQuantity, orderPrice, orderLocation, orderContractTo, "by", sess.CharacterName)

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

	// Open orders are split into buy and sell tables; confirmed orders get
	// their own collapsible table below the pending ones.
	var buyOrders, sellOrders, completedOrders []db.Order
	for _, order := range orders {
		switch {
		case order.Completed:
			completedOrders = append(completedOrders, order)
		case order.IsBuyOrder:
			buyOrders = append(buyOrders, order)
		default:
			sellOrders = append(sellOrders, order)
		}
	}

	data := struct {
		LoggedIn        bool
		CharacterName   string
		BuyOrders       []db.Order
		SellOrders      []db.Order
		CompletedOrders []db.Order
	}{LoggedIn: loggedIn, CharacterName: sess.CharacterName, BuyOrders: buyOrders, SellOrders: sellOrders, CompletedOrders: completedOrders}
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

// orderTotalFromForm reads the "price per item" and "price total" fields of
// the Create Order and Manage Order forms. price-basis is set by the page's
// script to whichever price field was typed in last.
func orderTotalFromForm(r *http.Request, quantity int64) (float64, error) {
	return orderTotal(r.PostFormValue("order-price-unit"), r.PostFormValue("order-price-total"), r.PostFormValue("price-basis"), quantity)
}

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

	orderQuantity, err := parseQuantity(r.PostFormValue("order-quantity"))
	if err != nil {
		http.Error(w, err.Error()+" (e.g. 100, 1,200 or 1.2k)", http.StatusBadRequest)
		return
	}

	// The form pre-fills prices with thousands separators (e.g. "8,138,285,000");
	// parsePrice also accepts k/M/B shorthand such as "40.34M".
	orderPrice, err := orderTotalFromForm(r, orderQuantity)
	if err != nil {
		http.Error(w, err.Error()+" (e.g. 40.34M, 1.5k, 8.5B or 8,138,285,000)", http.StatusBadRequest)
		return
	}

	orderLocation := strings.TrimSpace(r.PostFormValue("order-location"))
	if orderLocation == "" || len(orderLocation) > maxOrderTextLen {
		http.Error(w, fmt.Sprintf("location is required and must be at most %d characters", maxOrderTextLen), http.StatusBadRequest)
		return
	}

	// Optional: sell orders have no Contract To field at all.
	orderContractTo := strings.TrimSpace(r.PostFormValue("order-contract-to"))
	if len(orderContractTo) > maxOrderTextLen {
		http.Error(w, fmt.Sprintf("contract to must be at most %d characters", maxOrderTextLen), http.StatusBadRequest)
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

// orderBoardURL returns to the Buy or Sell Orders table the form was posted
// from, using its hidden side field.
func orderBoardURL(r *http.Request) string {
	if r.PostFormValue("side") == "sell" {
		return "/order-board?side=sell"
	}
	return "/order-board?side=buy"
}

func handleClaimOrder(w http.ResponseWriter, r *http.Request) {
	handleClaimChange(w, r, "claimed", orderBoardURL(r), db.ClaimOrder)
}

func handleUnclaimOrder(w http.ResponseWriter, r *http.Request) {
	handleClaimChange(w, r, "unclaimed", orderBoardURL(r), db.UnclaimOrder)
}

func handleForceUnclaimOrder(w http.ResponseWriter, r *http.Request) {
	handleClaimChange(w, r, "force-unclaimed", "/user-orders", db.ForceUnclaimOrder)
}
