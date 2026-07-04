package main

import (
	"QS-Indy/src/db"
	"QS-Indy/src/esi"
	"encoding/base64"
	"encoding/json"
	"log"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"github.com/tidwall/gjson"
)

func main() {
	err := godotenv.Load("config/.env")
	if err != nil {
		log.Fatal("Error loading .env file: ", err)
	}

	esiToken, err := esi.RefreshToken()
	if err != nil {
		log.Fatal(err)
	}

	parts := strings.Split(esiToken.AccessToken, ".")
	middle := parts[1]

	decodedBytes, err := base64.RawURLEncoding.DecodeString(middle)

	var result map[string]interface{}
	err = json.Unmarshal(decodedBytes, &result)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Decode Results:", result)

	characterIdString := gjson.GetBytes(decodedBytes, "sub").String()
	log.Println("Character ID String:", characterIdString)

	lastColon := strings.LastIndex(characterIdString, ":")
	idString := characterIdString[lastColon+1:]
	characterId, err := strconv.Atoi(idString)
	log.Println("Character ID:", characterId)

	accessToken := esiToken.AccessToken

	log.Print("Attempting to use AT:\n" + accessToken)

	corporationId, err := esi.GetCharacterCorporation(characterId)
	if err != nil {
		log.Fatal(err)
	}
	corporationBlueprints, err := esi.QueryCorporationBlueprints(corporationId, accessToken)

	log.Println("Attempting to save", len(corporationBlueprints), "blueprints")

	err = db.SaveBlueprintData(corporationBlueprints)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Successfully saved", len(corporationBlueprints), "blueprints")

}
