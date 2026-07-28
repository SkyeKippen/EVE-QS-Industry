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
}

var (
	itemsCache map[int]Item
	itemsOnce  sync.Once
	itemsErr   error
)

func ProcessOrderCreation(orderItem string, orderQuantity int64, orderPrice float64, orderLocation string, orderContractTo string, orderCreatedBy string) (int, error) {

	orderTypeId, err := mapNameToId(orderItem)
	if orderTypeId == 0 {
		log.Println("Invalid Item Name")
		return 0, errors.New("invalid Item Name")
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
		`SELECT * FROM meadow_works.industry_orders
			WHERE order_fulfilled IS FALSE
			ORDER BY internal_order_id`)
	defer rows.Close()
	log.Println("DB Query took", time.Since(start))

	start = time.Now()
	var allOrders []Order
	for rows.Next() {
		var order Order
		err = rows.Scan(&order.InternalIdCounter, &order.TypeId, &order.Quantity, &order.Price, &order.Location, &order.ContractTo, &order.CreatedBy, &order.Fulfilled, &order.Denied)
		if err != nil {
			return nil, err
		}
		order.TypeName, err = mapIdToName(order.TypeId)

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

	var order Order
	err = conn.QueryRow(context.Background(),
		`SELECT *
		FROM meadow_works.industry_orders
		WHERE internal_order_id = $1`,
		orderId,
	).Scan(&order.InternalIdCounter, &order.TypeId, &order.Price, &order.Quantity, &order.Location, &order.ContractTo, &order.CreatedBy, &order.Fulfilled, &order.Denied)

	if err != nil {
		return Order{}, err
	}

	order.TypeName, err = mapIdToName(order.TypeId)

	return order, nil
}

func MarkOrderFulfilled(orderId int) error {
	conn, err := connectDB()
	if err != nil {
		return err
	}

	_, err = conn.Exec(context.Background(),
		`UPDATE meadow_works.industry_orders
			SET order_fulfilled = true
			WHERE internal_order_id = $1`,
		orderId)
	if err != nil {
		return err
	}

	return nil
}

func LoadUserOrders(sess *auth.Session) ([]Order, error) {
	conn, err := connectDB()
	if err != nil {
		return nil, err
	}

	rows, err := conn.Query(context.Background(),
		`SELECT * FROM meadow_works.industry_orders
			WHERE order_created_by = $1
			ORDER BY internal_order_id`,
		sess.CharacterName)
	defer rows.Close()

	var userOrders []Order
	for rows.Next() {
		var order Order
		err = rows.Scan(&order.InternalIdCounter, &order.TypeId, &order.Quantity, &order.Price, &order.Location, &order.ContractTo, &order.CreatedBy, &order.Fulfilled, &order.Denied)
		if err != nil {
			return nil, err
		}
		order.TypeName, err = mapIdToName(order.TypeId)

		userOrders = append(userOrders, order)
	}
	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}

	return userOrders, nil
}

func ProcessOrderModification(sess *auth.Session, internalIdCounter int, orderQuantity int64, orderPrice float64, orderLocation string, orderContractTo string) error {
	conn, err := connectDB()
	if err != nil {
		return err
	}

	log.Println("Updating Order ID:", internalIdCounter)
	log.Println("With values (QTY, PRICE, LOC, CONTRACT_TO):", orderQuantity, orderPrice, orderLocation, orderContractTo)

	_, err = conn.Exec(context.Background(),
		`UPDATE meadow_works.industry_orders SET
		order_quantity = $2,
		order_price = $3, 
		order_location = $4,
		order_contract_to = $5
    	WHERE internal_order_id = $1`,
		internalIdCounter, orderQuantity, orderPrice, orderLocation, orderContractTo)
	if err != nil {
		log.Println("Encountered Error Updating order in DB:", err)
		return err
	}

	log.Println("Modified order with the values:", internalIdCounter, orderQuantity, orderPrice, orderLocation, orderContractTo, "by", sess.CharacterName)

	return nil
}

func DeleteOrder(orderId int) error {
	conn, err := connectDB()
	if err != nil {
		return err
	}

	log.Println("Deleting order:", orderId)

	_, err = conn.Exec(context.Background(),
		`DELETE FROM meadow_works.industry_orders
			WHERE internal_order_id = $1`, orderId)

	if err != nil {
		return err
	}
	return nil
}

func LoadUserFulfilledOrders(sess *auth.Session) ([]Order, error) {
	conn, err := connectDB()
	if err != nil {
		return nil, err
	}

	rows, err := conn.Query(context.Background(),
		`SELECT * FROM meadow_works.industry_orders
			WHERE order_created_by = $1
			AND order_fulfilled = true
			AND order_denied = false
			ORDER BY internal_order_id`,
		sess.CharacterName)
	defer rows.Close()

	var userFulfilledOrders []Order
	for rows.Next() {
		var order Order
		err = rows.Scan(&order.InternalIdCounter, &order.TypeId, &order.Quantity, &order.Price, &order.Location, &order.ContractTo, &order.CreatedBy, &order.Fulfilled, &order.Denied)
		if err != nil {
			return nil, err
		}
		order.TypeName, err = mapIdToName(order.TypeId)

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
		SET order_fulfilled = false
        AND order_denied = true
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
		SET order_fulfilled = false
        AND order_denied = true
    	WHERE internal_order_id = $1`,
		internalIdCounter)
	if err != nil {
		log.Println("Encountered Error Updating order in DB:", err)
		return err
	}

	return nil
}
