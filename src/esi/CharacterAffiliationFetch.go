package esi

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

type Character struct {
	Birthday       string  `json:"birthday"`
	BloodlineId    int64   `json:"bloodline_id"`
	CorporationId  int64   `json:"corporation_id"`
	Gender         string  `json:"gender"`
	Name           string  `json:"name"`
	RaceId         int64   `json:"race_id"`
	AllianceId     int64   `json:"alliance_id"`
	Description    string  `json:"description"`
	SecurityStatus float64 `json:"security_status"`
}

func GetCharacterCorporation(characterId int) (corporationId int64, err error) {
	client := &http.Client{}

	url := fmt.Sprintf("https://esi.evetech.net/characters/%d", characterId)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)

	log.Println("Querying:", url)

	response, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return 0, err
	}

	log.Println("Response Code:", response.Status)

	if response.StatusCode != http.StatusOK {
		return 0, esiStatusError(url, response.Status, body)
	}

	var character Character
	if err := json.Unmarshal(body, &character); err != nil {
		return 0, fmt.Errorf("could not parse character %d: %w", characterId, err)
	}

	return character.CorporationId, nil
}

// esiStatusError builds an error for a non-200 ESI response, including the
// response body since ESI puts its error message there.
func esiStatusError(url, status string, body []byte) error {
	return fmt.Errorf("esi: %s returned %s: %s", url, status, string(body))
}
