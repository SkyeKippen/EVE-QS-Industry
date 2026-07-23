package db

import (
	"QS-Indy/src/auth"
	"bufio"
	"context"
	"encoding/json"
	"log"
	"os"
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
}

func ProcessOrderCreation(orderItem string, orderQuantity int64, orderPrice int64, orderLocation string, orderContractTo string, orderCreatedBy string) error {

	orderTypeId, err := mapNameToId(orderItem)
	if err != nil {
		return err
	}

	conn, err := connectDB()
	if err != nil {
		return err
	}

	internalIdCounter := 0

	err = conn.QueryRow(context.Background(),
		`SELECT max(internal_order_id) FROM meadow_works.industry_orders`).Scan(&internalIdCounter)
	if err != nil {
		log.Println("Encountered Error Fetching maximum internal order ID:", err)
		return err
	}

	internalIdCounter++

	_, err = conn.Exec(context.Background(),
		`INSERT INTO meadow_works.industry_orders
		(internal_order_id, order_type_id, order_quantity, order_price, order_location, order_contract_to, order_created_by, order_fulfilled)
    	VALUES ($1, $2, $3, $4, $5, $6, $7, false)
    	ON CONFLICT DO NOTHING`,
		internalIdCounter, orderTypeId, orderQuantity, orderPrice, orderLocation, orderContractTo, orderCreatedBy)
	if err != nil {
		log.Println("Encountered Error Inserting order into DB:", err)
		return err
	}
	return nil
}

func LoadAllIndustryOrders() ([]Order, error) {
	conn, err := connectDB()
	if err != nil {
		return nil, err
	}

	rows, err := conn.Query(context.Background(),
		`SELECT * FROM meadow_works.industry_orders
			WHERE order_fulfilled IS FALSE`)
	defer rows.Close()

	var allOrders []Order
	for rows.Next() {
		var order Order
		err = rows.Scan(&order.InternalIdCounter, &order.TypeId, &order.Quantity, &order.Price, &order.Location, &order.ContractTo, &order.CreatedBy, &order.Fulfilled)
		if err != nil {
			return nil, err
		}
		order.TypeName, err = mapIdToName(order.TypeId)

		allOrders = append(allOrders, order)
	}
	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}

	return allOrders, nil
}

func mapNameToId(itemName string) (int, error) {
	items, err := loadItems("./data/sde/types.jsonl")
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
	items, err := loadItems("./data/sde/types.jsonl")
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

func loadItems(path string) (map[int]Item, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

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
		`SELECT internal_order_id, order_type_id, order_price, order_quantity, order_location, order_contract_to, order_created_by, order_fulfilled
		FROM meadow_works.industry_orders
		WHERE internal_order_id = $1`,
		orderId,
	).Scan(&order.InternalIdCounter, &order.TypeId, &order.Price, &order.Quantity, &order.Location, &order.ContractTo, &order.CreatedBy, &order.Fulfilled)

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
			WHERE order_created_by = $1`,
		sess.CharacterName)
	defer rows.Close()

	var userOrders []Order
	for rows.Next() {
		var order Order
		err = rows.Scan(&order.InternalIdCounter, &order.TypeId, &order.Quantity, &order.Price, &order.Location, &order.ContractTo, &order.CreatedBy, &order.Fulfilled)
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
