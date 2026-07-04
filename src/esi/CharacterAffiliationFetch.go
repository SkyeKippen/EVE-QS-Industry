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
		log.Fatal(err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "MWHI Project (admin contact: skyemeadows20@gmail.com)")

	log.Println("Querying:", url)

	response, err := client.Do(req)

	if err != nil {
		log.Println("Error:", err)
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
	}

	log.Println("Response Code:", response.Status)
	log.Println("Response Header:", response.Header)
	log.Println("Response Body:", string(body))

	var character Character
	err = json.Unmarshal(body, &character)
	if err != nil {
		log.Println("Error:", err)
	}

	return character.CorporationId, nil
}
