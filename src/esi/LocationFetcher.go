package esi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
)

const esiBaseURL = "https://esi.evetech.net"

// ErrForbidden means ESI refused the request with a 403. For structures this
// is what comes back when the character isn't on the structure's access
// list, or the structure no longer exists.
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
