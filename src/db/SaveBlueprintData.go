package db

import (
	"QS-Indy/src/esi"
	"context"
	"log"
	"os"

	"github.com/jackc/pgx/v5"
)

func connectDB() (*pgx.Conn, error) {
	conn, err := pgx.Connect(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func SaveBlueprintData(blueprints []esi.Blueprint) error {
	conn, err := connectDB()
	if err != nil {
		log.Println("Error:", err)
	}

	for _, blueprint := range blueprints {
		_, err := conn.Exec(context.Background(),
			`INSERT INTO meadow_works.blueprints
    		(item_id, location_flag, location_id, material_efficiency, quantity, runs, time_efficiency, type_id)
    		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (item_id) DO UPDATE
			SET location_flag = $2, location_id = $3, material_efficiency = $4, quantity = $5, runs = $6, time_efficiency = $7`,

			blueprint.ItemId, blueprint.LocationFlag, blueprint.LocationId, blueprint.MaterialEfficiency,
			blueprint.Quantity, blueprint.Runs, blueprint.TimeEfficiency, blueprint.TypeId)

		if err != nil {
			log.Println("Error:", err)
		}
	}
	return nil
}
