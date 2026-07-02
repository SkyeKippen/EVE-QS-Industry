package esi

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
)

type Blueprint struct {
	ItemId             int64  `json:"item_id"`
	LocationFlag       string `json:"location_flag"`
	LocationId         int64  `json:"location_id"`
	MaterialEfficiency int64  `json:"material_efficiency"`
	Quantity           int64  `json:"quantity"` // -1 is original, -2 is copy, >0 is a stack of unused BPs
	Runs               int64  `json:"runs"`
	TimeEfficiency     int64  `json:"time_efficiency"`
	TypeId             int64  `json:"type_id"`
}

func QueryCharacterBlueprints(characterId int, accessToken string) (blueprints []Blueprint, err error) {
	client := &http.Client{}

	allBlueprints := make([]Blueprint, 0)

	pagesCompleted := 0
	maxPages := 1 // will be overwritten using data from first page
	onPage := 1

	// token limit of 600 per 15 minutes

	for pagesCompleted < maxPages {

		url := fmt.Sprintf("https://esi.evetech.net/characters/%d/blueprints", characterId)

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			log.Fatal(err)
		}

		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "MWHI Project (admin contact: skyemeadows20@gmail.com)")

		log.Println("Querying:", url)

		response, err := client.Do(req)
		if err != nil {
			log.Println("Error:", err)
			return nil, err
		}

		defer func(Body io.ReadCloser) {
			err := Body.Close()
			if err != nil {
				log.Println("Error:", err)
			}
		}(response.Body)

		body, err := io.ReadAll(response.Body)
		if err != nil {
			log.Println("Error:", err)
			return nil, err
		}

		log.Println("Response Code:", response.Status)
		log.Println("Response Header:", response.Header)
		log.Println("Response Body:", string(body))

		var blueprint []Blueprint
		err = json.Unmarshal(body, &blueprint)
		if err != nil {
			log.Println("Error:", err)
			return nil, err
		}

		allBlueprints = append(allBlueprints, blueprint...)

		totalPagesStr := response.Header.Get("X-Pages")
		totalPages, err := strconv.Atoi(totalPagesStr)
		if err != nil {
			log.Println("Error:", err)
			return nil, err
		}

		maxPages = totalPages

		pagesCompleted++
		onPage++

		log.Println(fmt.Sprintf("Page Complete, current page count: %d of %d", pagesCompleted, maxPages))
	}

	return allBlueprints, nil
}
