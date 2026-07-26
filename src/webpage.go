package main

import (
	"QS-Indy/src/auth"
	"QS-Indy/src/db"
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
	http.HandleFunc("/process-order-creation", auth.RequireAuth(renderOrderCreation))

	http.HandleFunc("/fulfill-order", auth.RequireAuth(renderFulfillOrder))

	http.HandleFunc("/fulfill-order/confirm", auth.RequireAuth(handleFulfillOrderConfirm))

	http.HandleFunc("/user-orders", auth.RequireAuth(renderUserOrders))
	http.HandleFunc("/manage-order", auth.RequireAuth(renderManageOrder))
	http.HandleFunc("/manage-order/submit-changes", auth.RequireAuth(renderSubmitOrderChanges))
	http.HandleFunc("/manage-order/delete-order", auth.RequireAuth(renderDeleteOrder))

	http.HandleFunc("/auth/login", auth.HandleLogin)
	http.HandleFunc("/auth/callback", auth.HandleCallback)
	http.HandleFunc("/auth/logout", handleLogout)

	log.Println("Server running at https://qsindy.skyemeadows.net (port 5001)")
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
	start := time.Now()
	orders, err := db.LoadAllIndustryOrders()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Println("Time to process LoadAllIndustryOrders:", time.Since(start))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = tmpl.ExecuteTemplate(w, "order_board.html", orders)
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

func renderOrderCreation(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sess, _ := auth.CurrentSession(r)

	orderItem := strings.TrimSpace(r.PostFormValue("order-item"))
	orderQuantity, err := strconv.ParseInt(r.PostFormValue("order-quantity"), 10, 64)
	orderPrice, err := strconv.ParseFloat(r.PostFormValue("order-price"), 64)
	orderLocation := strings.TrimSpace(r.PostFormValue("order-location"))
	orderContractTo := strings.TrimSpace(r.PostFormValue("order-contract-to"))

	if len(orderItem) > 50 {
		http.Error(w, "Item Name field exceeds maximum length", http.StatusBadRequest)
		return
	}

	if len(orderLocation) > 50 {
		http.Error(w, "Location field exceeds maximum length", http.StatusBadRequest)
		return
	}

	if len(orderContractTo) > 50 {
		http.Error(w, "Contract To field exceeds maximum length", http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	orderId, err := db.ProcessOrderCreation(orderItem, orderQuantity, orderPrice, orderLocation, orderContractTo, sess.CharacterName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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

	err = db.MarkOrderFulfilled(orderId)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sess, _ := auth.CurrentSession(r)

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

	data := struct {
		LoggedIn      bool
		CharacterName string
		Orders        []db.Order
		ReadyOrders   []db.Order
	}{LoggedIn: loggedIn, CharacterName: sess.CharacterName, Orders: orders, ReadyOrders: userFulfilledOrders}
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

func renderSubmitOrderChanges(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sess, _ := auth.CurrentSession(r)

	orderQuantity, err := strconv.ParseInt(r.PostFormValue("order-quantity"), 10, 64)
	orderPrice, err := strconv.ParseFloat(r.PostFormValue("order-price"), 64)
	orderLocation := r.PostFormValue("order-location")
	orderContractTo := r.PostFormValue("order-contract-to")
	orderInternalId64, err := strconv.ParseInt(r.PostFormValue("order-id"), 10, 64)
	orderInternalId := int(orderInternalId64)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		log.Println("Error parsing order-item:", err.Error())
		return
	}

	log.Println("Got values from forums as:", orderQuantity, orderPrice, orderLocation, orderContractTo, "by", sess.CharacterName)

	err = db.ProcessOrderModification(sess, orderInternalId, orderQuantity, orderPrice, orderLocation, orderContractTo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.Redirect(w, r, "/user-orders", http.StatusSeeOther)
}

func renderDeleteOrder(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sess, _ := auth.CurrentSession(r)

	orderInternalId64, err := strconv.ParseInt(r.PostFormValue("order-id"), 10, 64)

	log.Println("DELETING ORDER", orderInternalId64, "AUTHORIZED BY", sess.CharacterName)

	err = db.DeleteOrder(int(orderInternalId64))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.Redirect(w, r, "/user-orders", http.StatusSeeOther)
}
