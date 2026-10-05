package esi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"strconv"
)

const esiBaseURL = "https://esi.evetech.net"

// ErrForbidden means ESI refused the request with a 403. For structures this
// is what comes back when the character isn't on the structure's access
// list, or the structure no longer exists. For assets it usually means the
// token is missing the scope or the character lacks the corporation role.
var ErrForbidden = errors.New("esi: forbidden")

// ErrNotFound means ESI answered 404.
var ErrNotFound = errors.New("esi: not found")

type Station struct {
	Name     string `json:"name"`
	Owner    int64  `json:"owner"` // NPC corporation
	SystemId int64  `json:"system_id"`
	TypeId   int64  `json:"type_id"`
}

type Structure struct {
	Name          string `json:"name"`
	OwnerId       int64  `json:"owner_id"` // corporation
	SolarSystemId int64  `json:"solar_system_id"`
	TypeId        int64  `json:"type_id"`
}

type SolarSystem struct {
	Name            string `json:"name"`
	ConstellationId int64  `json:"constellation_id"`
}

type Constellation struct {
	Name     string `json:"name"`
	RegionId int64  `json:"region_id"`
}

type Asset struct {
	ItemId       int64  `json:"item_id"`
	LocationFlag string `json:"location_flag"`
	LocationId   int64  `json:"location_id"`
	LocationType string `json:"location_type"` // station, solar_system, item or other
	TypeId       int64  `json:"type_id"`
	IsSingleton  bool   `json:"is_singleton"`
}

// Client makes the ESI calls the location lookup needs.
type Client struct {
	HTTPClient *http.Client
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (c *Client) GetStation(ctx context.Context, stationId int64) (Station, error) {
	var station Station
	_, err := c.do(ctx, "GET", fmt.Sprintf("%s/universe/stations/%d", esiBaseURL, stationId), "", nil, &station)
	return station, err
}

// GetStructure needs the esi-universe.read_structures.v1 scope.
func (c *Client) GetStructure(ctx context.Context, structureId int64, accessToken string) (Structure, error) {
	var structure Structure
	_, err := c.do(ctx, "GET", fmt.Sprintf("%s/universe/structures/%d", esiBaseURL, structureId), accessToken, nil, &structure)
	return structure, err
}

func (c *Client) GetSolarSystem(ctx context.Context, systemId int64) (SolarSystem, error) {
	var system SolarSystem
	_, err := c.do(ctx, "GET", fmt.Sprintf("%s/universe/systems/%d", esiBaseURL, systemId), "", nil, &system)
	return system, err
}

func (c *Client) GetConstellation(ctx context.Context, constellationId int64) (Constellation, error) {
	var constellation Constellation
	_, err := c.do(ctx, "GET", fmt.Sprintf("%s/universe/constellations/%d", esiBaseURL, constellationId), "", nil, &constellation)
	return constellation, err
}

// GetNames looks up the names of characters, corporations, alliances,
// systems, regions, types and stations by ID.
func (c *Client) GetNames(ctx context.Context, ids []int64) (map[int64]string, error) {
	names := make(map[int64]string, len(ids))
	for _, chunk := range chunkIds(uniqueIds(ids), 1000) {
		var resolved []struct {
			Id   int64  `json:"id"`
			Name string `json:"name"`
		}
		if _, err := c.do(ctx, "POST", esiBaseURL+"/universe/names", "", chunk, &resolved); err != nil {
			return nil, err
		}
		for _, entry := range resolved {
			names[entry.Id] = entry.Name
		}
	}
	return names, nil
}

// GetCorporationAssets needs the esi-assets.read_corporation_assets.v1
// scope and the Director role.
func (c *Client) GetCorporationAssets(ctx context.Context, corporationId int64, accessToken string) ([]Asset, error) {
	return c.getAssetPages(ctx, fmt.Sprintf("%s/corporations/%d/assets", esiBaseURL, corporationId), accessToken)
}

func (c *Client) GetCorporationAssetNames(ctx context.Context, corporationId int64, accessToken string, itemIds []int64) (map[int64]string, error) {
	return c.getAssetNames(ctx, fmt.Sprintf("%s/corporations/%d/assets/names", esiBaseURL, corporationId), accessToken, itemIds)
}

func (c *Client) getAssetPages(ctx context.Context, baseUrl string, accessToken string) ([]Asset, error) {
	allAssets := make([]Asset, 0)
	maxPages := 1 // will be overwritten using data from first page
	for onPage := 1; onPage <= maxPages; onPage++ {
		var assets []Asset
		header, err := c.do(ctx, "GET", fmt.Sprintf("%s?page=%d", baseUrl, onPage), accessToken, nil, &assets)
		if err != nil {
			return nil, err
		}
		allAssets = append(allAssets, assets...)

		if totalPagesStr := header.Get("X-Pages"); totalPagesStr != "" {
			maxPages, err = strconv.Atoi(totalPagesStr)
			if err != nil {
				return nil, fmt.Errorf("invalid X-Pages header %q from %s: %w", totalPagesStr, baseUrl, err)
			}
		}
	}
	return allAssets, nil
}

func (c *Client) getAssetNames(ctx context.Context, url string, accessToken string, itemIds []int64) (map[int64]string, error) {
	names := make(map[int64]string, len(itemIds))
	for _, chunk := range chunkIds(uniqueIds(itemIds), 1000) {
		if err := c.getAssetNameChunk(ctx, url, accessToken, chunk, names); err != nil {
			return nil, err
		}
	}
	return names, nil
}

// getAssetNameChunk names one chunk of items. ESI rejects the whole request
// with a 404 when any ID is no longer a valid asset (the asset list is cached
// for up to an hour, so a container can be gone by now), so on a 404 the
// chunk is split in half until the bad IDs are found and skipped.
func (c *Client) getAssetNameChunk(ctx context.Context, url string, accessToken string, chunk []int64, names map[int64]string) error {
	var resolved []struct {
		ItemId int64  `json:"item_id"`
		Name   string `json:"name"`
	}
	_, err := c.do(ctx, "POST", url, accessToken, chunk, &resolved)
	if errors.Is(err, ErrNotFound) {
		if len(chunk) == 1 {
			log.Printf("esi: skipping item %d, which can't be named: %v", chunk[0], err)
			return nil
		}
		half := len(chunk) / 2
		if err := c.getAssetNameChunk(ctx, url, accessToken, chunk[:half], names); err != nil {
			return err
		}
		return c.getAssetNameChunk(ctx, url, accessToken, chunk[half:], names)
	}
	if err != nil {
		return err
	}

	for _, entry := range resolved {
		// ESI uses "None" for items that were never named
		if entry.Name != "" && entry.Name != "None" {
			names[entry.ItemId] = html.UnescapeString(entry.Name) // ESI escapes names like "T1 &gt;&gt; T2"
		}
	}
	return nil
}

// do sends one ESI request, sending body as JSON when it isn't nil, and
// parses the JSON response into out. 403 and 404 come back wrapped in
// ErrForbidden and ErrNotFound so callers can tell them apart.
func (c *Client) do(ctx context.Context, method, url, accessToken string, body any, out any) (http.Header, error) {
	var reqBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}

	log.Println("Querying:", method, url)

	response, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	respBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusForbidden:
		return nil, fmt.Errorf("%w: %w", ErrForbidden, esiStatusError(url, response.Status, respBody))
	case http.StatusNotFound:
		return nil, fmt.Errorf("%w: %w", ErrNotFound, esiStatusError(url, response.Status, respBody))
	default:
		return nil, esiStatusError(url, response.Status, respBody)
	}

	if err := json.Unmarshal(respBody, out); err != nil {
		return nil, fmt.Errorf("could not parse response from %s: %w", url, err)
	}
	return response.Header, nil
}

func uniqueIds(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	unique := make([]int64, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	return unique
}

func chunkIds(ids []int64, size int) [][]int64 {
	chunks := make([][]int64, 0, len(ids)/size+1)
	for len(ids) > size {
		chunks = append(chunks, ids[:size])
		ids = ids[size:]
	}
	if len(ids) > 0 {
		chunks = append(chunks, ids)
	}
	return chunks
}
