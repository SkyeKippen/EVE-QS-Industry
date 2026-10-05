package esi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

type resolvedName struct {
	Id       int64  `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
}

// GetNames resolves character, corporation and other entity IDs to their
// names with the public /universe/names route.
func GetNames(ids []int64) (map[int64]string, error) {
	names := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return names, nil
	}

	payload, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}

	url := "https://esi.evetech.net/universe/names"

	req, err := http.NewRequest("POST", url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", UserAgent)

	log.Println("Querying:", url)

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	log.Println("Response Code:", response.Status)

	if response.StatusCode != http.StatusOK {
		return nil, esiStatusError(url, response.Status, body)
	}

	var resolved []resolvedName
	if err := json.Unmarshal(body, &resolved); err != nil {
		return nil, fmt.Errorf("could not parse names: %w", err)
	}

	for _, r := range resolved {
		names[r.Id] = r.Name
	}
	return names, nil
}
