package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
)

func announceStarIfEnabled(username, reason string) {
	if getSetting("ha_enabled") != "1" {
		return
	}
	haURL := getSetting("ha_url")
	haToken := getSetting("ha_token")
	haEntity := getSetting("ha_media_player")
	if haURL == "" || haToken == "" || haEntity == "" {
		return
	}

	haURL = strings.TrimRight(haURL, "/")
	message := fmt.Sprintf("%s got a star for %s!", username, reason)

	go func() {
		payload := map[string]interface{}{
			"entity_id": haEntity,
			"message":   message,
		}
		body, _ := json.Marshal(payload)

		url := haURL + "/api/services/tts/microsoft_say"
		req, err := http.NewRequest("POST", url, bytes.NewReader(body))
		if err != nil {
			log.Printf("HA announce error: %v", err)
			return
		}
		req.Header.Set("Authorization", "Bearer "+haToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("HA announce error: %v", err)
			return
		}
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			log.Printf("HA announce returned status %d", resp.StatusCode)
		}
	}()
}
