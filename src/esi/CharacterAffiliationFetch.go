package esi

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
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

// characters rarely change corporation, so lookups are reused for an hour
const corporationCacheTTL = time.Hour

type cachedCorporation struct {
	corporationId int64
	fetchedAt     time.Time
}

var (
	corporationCacheMu sync.Mutex
	corporationCache   = map[int]cachedCorporation{}
)

// GetCharacterCorporationCached is GetCharacterCorporation with an in-memory
// cache, for page handlers that need it on every request.
func GetCharacterCorporationCached(characterId int) (int64, error) {
	corporationCacheMu.Lock()
	cached, ok := corporationCache[characterId]
	corporationCacheMu.Unlock()
	if ok && time.Since(cached.fetchedAt) < corporationCacheTTL {
		return cached.corporationId, nil
	}

	corporationId, err := GetCharacterCorporation(characterId)
	if err != nil {
		return 0, err
	}

	corporationCacheMu.Lock()
	corporationCache[characterId] = cachedCorporation{corporationId: corporationId, fetchedAt: time.Now()}
	corporationCacheMu.Unlock()
	return corporationId, nil
}
