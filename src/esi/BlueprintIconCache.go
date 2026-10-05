package esi

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

// Blueprint icons come from the EVE image server, which every blueprint and
// reaction formula has a "bp" (original) and "bpc" (copy) icon on. Each one
// is downloaded once and kept on disk, so the image server is only asked
// about icons this app hasn't seen before. Delete the folder to refetch them.
const (
	blueprintIconDir  = "data/icons/blueprints"
	blueprintIconSize = 64 // shown at 32px, so this stays sharp on high-DPI screens
)

// ErrIconNotFound means the image server has no icon for that type.
var ErrIconNotFound = errors.New("no icon for this type")

var (
	iconLocks  sync.Map // cache file path -> *sync.Mutex, so one icon downloads once
	iconMisses sync.Map // cache file path -> true, for types the image server lacks
)

// BlueprintIconPath returns the path of the cached icon for typeId,
// downloading it first if needed. isCopy picks the blueprint copy icon.
func BlueprintIconPath(typeId int, isCopy bool) (string, error) {
	variation := "bp"
	if isCopy {
		variation = "bpc"
	}
	path := filepath.Join(blueprintIconDir, fmt.Sprintf("%d_%s_%d.png", typeId, variation, blueprintIconSize))

	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if _, missing := iconMisses.Load(path); missing {
		return "", ErrIconNotFound
	}

	lock, _ := iconLocks.LoadOrStore(path, &sync.Mutex{})
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()

	// another request may have downloaded it while this one waited
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	if err := downloadBlueprintIcon(typeId, variation, path); err != nil {
		if errors.Is(err, ErrIconNotFound) {
			iconMisses.Store(path, true)
		}
		return "", err
	}
	return path, nil
}

func downloadBlueprintIcon(typeId int, variation string, path string) error {
	url := fmt.Sprintf("https://images.evetech.net/types/%d/%s?size=%d", typeId, variation, blueprintIconSize)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)

	log.Println("Querying:", url)

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotFound {
		return ErrIconNotFound
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return esiStatusError(url, response.Status, body)
	}

	if err := os.MkdirAll(blueprintIconDir, 0755); err != nil {
		return err
	}

	// write to a temp file and rename, so a half-written icon is never served
	tmp, err := os.CreateTemp(blueprintIconDir, "download-*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
