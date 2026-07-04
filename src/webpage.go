package main

import (
	"QS-Indy/src/db"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

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

func main() {
	err := godotenv.Load("config/.env")
	if err != nil {
		log.Fatal("Error loading .env file: ", err)
	}
	_, thisFile, _, _ := runtime.Caller(0)
	baseDir := filepath.Dir(thisFile)
	staticDir := filepath.Join(baseDir, "static")

	fs := http.FileServer(http.Dir(staticDir))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	http.HandleFunc("/", renderBase)
	http.HandleFunc("/blueprints", renderBlueprints)
	http.HandleFunc("/order-board", renderOrderBoard)

	http.HandleFunc("/create-order", renderCreateOrder)
	http.HandleFunc("/process-order-creation", renderOrderCreation)

	log.Println("Server running at http://localhost:8080")
	http.ListenAndServe(":8080", nil)
}

func renderBase(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl.ExecuteTemplate(w, "base.html", nil)
}

func renderBlueprints(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl.ExecuteTemplate(w, "blueprints_browser.html", nil)
}

func renderOrderBoard(w http.ResponseWriter, r *http.Request) {
	orders, err := db.LoadAllIndustryOrders()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl.ExecuteTemplate(w, "order_board.html", orders)
}

func renderCreateOrder(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl.ExecuteTemplate(w, "create_order.html", nil)
}

func renderOrderCreation(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	orderItem := r.PostFormValue("order-item")
	orderQuantity, err := strconv.ParseInt(r.PostFormValue("order-quantity"), 10, 64)
	orderPrice, err := strconv.ParseInt(r.PostFormValue("order-price"), 10, 64)
	orderLocation := r.PostFormValue("order-location")
	orderContractTo := r.PostFormValue("order-contract-to")

	err = db.ProcessOrderCreation(orderItem, orderQuantity, orderPrice, orderLocation, orderContractTo)

	log.Println("Created order with the values:", orderItem, orderQuantity, orderPrice, orderLocation, orderContractTo)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl.ExecuteTemplate(w, "create_order.html", nil)
}
