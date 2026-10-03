package db

import (
	"QS-Indy/src/auth"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"sync"
	"time"
)

type Item struct {
	Key    int     `json:"_key"` // Is typeID
	Name   string  `json:"name"`
	IconId int     `json:"iconID"`
	Volume float64 `json:"volume"`
}

type Order struct {
	InternalIdCounter int     `json:"internalIDCounter"`
	TypeId            int     `json:"typeID"`
	TypeName          string  `json:"typeName"`
	Quantity          int     `json:"quantity"`
	Price             float64 `json:"price"`
	Location          string  `json:"location"`
	ContractTo        string  `json:"contractTo"`
	CreatedBy         string  `json:"createdBy"`
	Fulfilled         bool    `json:"fulfilled"`
	Denied            bool    `json:"denied"`
	Completed         bool    `json:"completed"`
	// ClaimedBy is the character holding an active (under 24 hours old)
	// claim on the order, or "" when nobody does.
	ClaimedBy string `json:"claimedBy"`
}

// claimActiveSQL is true for rows whose claim is under 24 hours old.
const claimActiveSQL = `(order_claimed_by IS NOT NULL AND order_claimed_at > now() - interval '24 hours')`

// orderColumns lists the columns scanOrder expects, in order. Expired
// claims come back as an empty claimer.
const orderColumns = `internal_order_id, order_type_id, order_quantity, order_price,
	order_location, order_contract_to, order_created_by,
	order_fulfilled, order_denied, order_completed,
	CASE WHEN ` + claimActiveSQL + ` THEN order_claimed_by ELSE '' END`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanOrder(row rowScanner) (Order, error) {
	var order Order
	err := row.Scan(&order.InternalIdCounter, &order.TypeId, &order.Quantity, &order.Price, &order.Location, &order.ContractTo, &order.CreatedBy, &order.Fulfilled, &order.Denied, &order.Completed, &order.ClaimedBy)
	if err != nil {
		return Order{}, err
	}
	order.TypeName, err = mapIdToName(order.TypeId)
	return order, err
}

var (
	itemsCache map[int]Item
	itemsOnce  sync.Once
	itemsErr   error
)

func ProcessOrderCreation(orderItem string, orderQuantity int64, orderPrice float64, orderLocation string, orderContractTo string, orderCreatedBy string) (int, error) {

	orderTypeId, err := mapNameToId(orderItem)
	if orderTypeId == 0 {
		log.Println("Invalid Item Name:", orderItem)
		return 0, errors.New("invalid item name")
	}

	if err != nil {
		return 0, err
	}

	conn, err := connectDB()
	if err != nil {
		return 0, err
	}

	internalIdCounter := 0

	err = conn.QueryRow(context.Background(),
		`SELECT COALESCE(MAX(internal_order_id),0) FROM meadow_works.industry_orders`).Scan(&internalIdCounter)
	if err != nil {
		log.Println("Encountered Error Fetching maximum internal order ID:", err)
		return 0, err
	}

	internalIdCounter++

	_, err = conn.Exec(context.Background(),
		`INSERT INTO meadow_works.industry_orders
		(internal_order_id, order_type_id, order_quantity, order_price, order_location, order_contract_to, order_created_by, order_fulfilled, order_denied)
    	VALUES ($1, $2, $3, $4, $5, $6, $7, false, false)
    	ON CONFLICT DO NOTHING`,
		internalIdCounter, orderTypeId, orderQuantity, orderPrice, orderLocation, orderContractTo, orderCreatedBy)
	if err != nil {
		log.Println("Encountered Error Inserting order into DB:", err)
		return 0, err
	}
	return internalIdCounter, nil
}

func LoadAllIndustryOrders() ([]Order, error) {
	start := time.Now()
	conn, err := connectDB()
	if err != nil {
		return nil, err
	}
	log.Println("Connecting to DB took", time.Since(start))

	start = time.Now()
	rows, err := conn.Query(context.Background(),
		`SELECT `+orderColumns+` FROM meadow_works.industry_orders
			WHERE order_fulfilled IS FALSE
			ORDER BY internal_order_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	log.Println("DB Query took", time.Since(start))

	start = time.Now()
	var allOrders []Order
	for rows.Next() {
		order, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}

		allOrders = append(allOrders, order)
	}
	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}
	log.Println("Mapping DB data to struct took", time.Since(start))

	return allOrders, nil
}

func mapNameToId(itemName string) (int, error) {
	items, err := getItems()
	if err != nil {
		return 0, err
	}

	for _, item := range items {
		if item.Name == itemName {
			return item.Key, nil
		}
	}

	return 0, nil
}

func mapIdToName(typeId int) (string, error) {
	items, err := getItems()
	if err != nil {
		return "", err
	}

	for _, item := range items {
		if item.Key == typeId {
			return item.Name, nil
		}
	}

	return "", nil
}

func getItems() (map[int]Item, error) {
	itemsOnce.Do(func() {
		itemsCache, itemsErr = loadItems("./data/sde/types.jsonl")
	})
	return itemsCache, itemsErr
}

func loadItems(path string) (map[int]Item, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {

		}
	}(file)

	lang := "en"

	items := make(map[int]Item)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var raw struct {
			Key    int               `json:"_key"`
			Name   map[string]string `json:"name"`
			IconId int               `json:"iconID"`
			Volume float64           `json:"volume"`
		}

		if err := json.Unmarshal(scanner.Bytes(), &raw); err != nil {
			log.Println("Skipping bad item line:", err)
			continue
		}

		item := Item{
			Key:    raw.Key,
			Name:   raw.Name[lang],
			IconId: raw.IconId,
			Volume: raw.Volume,
		}
		items[item.Key] = item
	}
	return items, scanner.Err()
}

func FetchOrderById(orderId int) (Order, error) {
	conn, err := connectDB()
	if err != nil {
		return Order{}, err
	}

	defer conn.Close(context.Background())

	return scanOrder(conn.QueryRow(context.Background(),
		`SELECT `+orderColumns+` FROM meadow_works.industry_orders
		WHERE internal_order_id = $1`,
		orderId))
}

// ErrOrderClaimed is returned when another character holds an active claim
// on the order.
var ErrOrderClaimed = errors.New("order is claimed by another pilot or no longer open")

// MarkOrderFulfilled marks an open order fulfilled by fulfilledBy, refusing
// if someone else holds an active claim. Fulfilling releases the claim so a
// denied fulfillment returns the order to the board unclaimed. It also
// clears order_denied left over from an earlier denied fulfillment.
func MarkOrderFulfilled(orderId int, fulfilledBy string) error {
	conn, err := connectDB()
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())

	tag, err := conn.Exec(context.Background(),
		`UPDATE meadow_works.industry_orders
			SET order_fulfilled = true,
			order_denied = false,
			order_claimed_by = NULL,
			order_claimed_at = NULL
			WHERE internal_order_id = $1
			AND order_fulfilled IS FALSE
			AND (NOT `+claimActiveSQL+` OR order_claimed_by = $2)`,
		orderId, fulfilledBy)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrOrderClaimed
	}

	return nil
}

// ErrClaimNotAllowed is returned when a claim, unclaim or force-unclaim does
// not apply: the order is gone, fulfilled, already claimed, or the caller is
// not the claimer or owner.
var ErrClaimNotAllowed = errors.New("order cannot be claimed or unclaimed right now")

// execClaimChange runs a claim update and maps "no row matched" to
// ErrClaimNotAllowed so every rule lives in the WHERE clause.
func execClaimChange(sql string, args ...any) error {
	conn, err := connectDB()
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())

	tag, err := conn.Exec(context.Background(), sql, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrClaimNotAllowed
	}
	return nil
}

// ClaimOrder reserves an open, unclaimed order for claimer for ClaimDuration.
func ClaimOrder(orderId int, claimer string) error {
	return execClaimChange(
		`UPDATE meadow_works.industry_orders
			SET order_claimed_by = $2, order_claimed_at = now()
			WHERE internal_order_id = $1
			AND order_fulfilled IS FALSE
			AND NOT `+claimActiveSQL,
		orderId, claimer)
}

// UnclaimOrder releases claimer's own active claim.
func UnclaimOrder(orderId int, claimer string) error {
	return execClaimChange(
		`UPDATE meadow_works.industry_orders
			SET order_claimed_by = NULL, order_claimed_at = NULL
			WHERE internal_order_id = $1
			AND order_claimed_by = $2
			AND `+claimActiveSQL,
		orderId, claimer)
}

// ForceUnclaimOrder lets the order's owner release whoever has claimed it.
func ForceUnclaimOrder(orderId int, owner string) error {
	return execClaimChange(
		`UPDATE meadow_works.industry_orders
			SET order_claimed_by = NULL, order_claimed_at = NULL
			WHERE internal_order_id = $1
			AND order_created_by = $2
			AND `+claimActiveSQL,
		orderId, owner)
}

func LoadUserOrders(sess *auth.Session) ([]Order, error) {
	conn, err := connectDB()
	if err != nil {
		return nil, err
	}

	rows, err := conn.Query(context.Background(),
		`SELECT `+orderColumns+` FROM meadow_works.industry_orders
			WHERE order_created_by = $1
			ORDER BY internal_order_id`,
		sess.CharacterName)
	defer rows.Close()

	var userOrders []Order
	for rows.Next() {
		order, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}

		userOrders = append(userOrders, order)
	}
	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}

	return userOrders, nil
}

// ErrOrderNotEditable is returned when an order does not exist, belongs to
// another character, has already been fulfilled, or is currently claimed.
var ErrOrderNotEditable = errors.New("order not found, not owned by you, already fulfilled, or claimed (force-unclaim it first)")

func ProcessOrderModification(sess *auth.Session, internalIdCounter int, orderQuantity int64, orderPrice float64, orderLocation string, orderContractTo string) error {
	conn, err := connectDB()
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())

	log.Println("Updating Order ID:", internalIdCounter)
	log.Println("With values (QTY, PRICE, LOC, CONTRACT_TO):", orderQuantity, orderPrice, orderLocation, orderContractTo)

	tag, err := conn.Exec(context.Background(),
		`UPDATE meadow_works.industry_orders SET
		order_quantity = $2,
		order_price = $3,
		order_location = $4,
		order_contract_to = $5
		WHERE internal_order_id = $1
		AND order_created_by = $6
		AND order_fulfilled IS FALSE
		AND NOT `+claimActiveSQL,
		internalIdCounter, orderQuantity, orderPrice, orderLocation, orderContractTo, sess.CharacterName)
	if err != nil {
		log.Println("Encountered Error Updating order in DB:", err)
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrOrderNotEditable
	}

	log.Println("Modified order with the values:", internalIdCounter, orderQuantity, orderPrice, orderLocation, orderContractTo, "by", sess.CharacterName)

	return nil
}

// DeleteOrder deletes an order owned by owner, under the same rules as
// editing: it must not be fulfilled or claimed.
func DeleteOrder(orderId int, owner string) error {
	conn, err := connectDB()
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())

	log.Println("Deleting order:", orderId)

	tag, err := conn.Exec(context.Background(),
		`DELETE FROM meadow_works.industry_orders
			WHERE internal_order_id = $1
			AND order_created_by = $2
			AND order_fulfilled IS FALSE
			AND NOT `+claimActiveSQL, orderId, owner)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrOrderNotEditable
	}
	return nil
}

func LoadUserFulfilledOrders(sess *auth.Session) ([]Order, error) {
	conn, err := connectDB()
	if err != nil {
		return nil, err
	}

	rows, err := conn.Query(context.Background(),
		`SELECT `+orderColumns+` FROM meadow_works.industry_orders
			WHERE order_created_by = $1
			AND order_fulfilled = true
			AND order_completed = false
			ORDER BY internal_order_id`,
		sess.CharacterName)
	defer rows.Close()

	var userFulfilledOrders []Order
	for rows.Next() {
		order, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}

		userFulfilledOrders = append(userFulfilledOrders, order)
	}
	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}

	return userFulfilledOrders, nil
}

func VerifyFulfilledOrder(sess *auth.Session, internalIdCounter int) error {
	conn, err := connectDB()
	if err != nil {
		return err
	}

	_, err = conn.Exec(context.Background(),
		`UPDATE meadow_works.industry_orders 
		SET order_completed = true,
		order_denied = false
    	WHERE internal_order_id = $1`,
		internalIdCounter)
	if err != nil {
		log.Println("Encountered Error Updating order in DB:", err)
		return err
	}

	return nil
}

func DenyFulfilledOrder(sess *auth.Session, internalIdCounter int) error {
	conn, err := connectDB()
	if err != nil {
		return err
	}

	_, err = conn.Exec(context.Background(),
		`UPDATE meadow_works.industry_orders 
		SET order_fulfilled = false,
        order_denied = true
    	WHERE internal_order_id = $1`,
		internalIdCounter)
	if err != nil {
		log.Println("Encountered Error Updating order in DB:", err)
		return err
	}

	return nil
}
