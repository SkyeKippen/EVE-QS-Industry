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
	Runs               int64  `json:"runs"`     // -1 is BPO/Infinite
	TimeEfficiency     int64  `json:"time_efficiency"`
	TypeId             int64  `json:"type_id"`
}

func QueryCharacterBlueprints(characterId int, accessToken string) (blueprints []Blueprint, err error) {
	return queryBlueprintPages(fmt.Sprintf("https://esi.evetech.net/characters/%d/blueprints", characterId), accessToken)
}

func QueryCorporationBlueprints(corporationId int64, accessToken string) (blueprints []Blueprint, err error) {
	return queryBlueprintPages(fmt.Sprintf("https://esi.evetech.net/corporations/%d/blueprints", corporationId), accessToken)
}

// queryBlueprintPages fetches every page of a paginated ESI blueprints
// endpoint, using the X-Pages header from each response to know when to stop.
func queryBlueprintPages(baseUrl string, accessToken string) ([]Blueprint, error) {
	client := &http.Client{}

	allBlueprints := make([]Blueprint, 0)

	maxPages := 1 // will be overwritten using data from first page

	// token limit of 600 per 15 minutes

	for onPage := 1; onPage <= maxPages; onPage++ {
		url := fmt.Sprintf("%s?page=%d", baseUrl, onPage)

		blueprints, totalPages, err := queryBlueprintPage(client, url, accessToken)
		if err != nil {
			return nil, err
		}

		allBlueprints = append(allBlueprints, blueprints...)
		maxPages = totalPages

		log.Println(fmt.Sprintf("Page Complete, current page count: %d of %d", onPage, maxPages))
	}

	return allBlueprints, nil
}

func queryBlueprintPage(client *http.Client, url string, accessToken string) (blueprints []Blueprint, totalPages int, err error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, 0, err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "MWHI Project (admin contact: skyemeadows20@gmail.com)")

	log.Println("Querying:", url)

	response, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, 0, err
	}

	log.Println("Response Code:", response.Status)

	if response.StatusCode != http.StatusOK {
		return nil, 0, esiStatusError(url, response.Status, body)
	}

	if err := json.Unmarshal(body, &blueprints); err != nil {
		return nil, 0, fmt.Errorf("could not parse blueprints from %s: %w", url, err)
	}

	// treat a missing X-Pages header as a single page
	totalPages = 1
	if totalPagesStr := response.Header.Get("X-Pages"); totalPagesStr != "" {
		totalPages, err = strconv.Atoi(totalPagesStr)
		if err != nil {
			return nil, 0, fmt.Errorf("invalid X-Pages header %q from %s: %w", totalPagesStr, url, err)
		}
	}

	return blueprints, totalPages, nil
}
