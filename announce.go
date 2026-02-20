package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
)

func announceStarIfEnabled(username string, reasonID *int, reasonText string, stars int) {
	if getSetting("ha_enabled") != "1" {
		return
	}
	haURL := getSetting("ha_url")
	haToken := getSetting("ha_token")
	haEntity := getSetting("ha_media_player")
	if haURL == "" || haToken == "" || haEntity == "" {
		return
	}

	// Get announce language setting
	lang := getSetting("ha_lang")
	if lang == "" {
		lang = "en"
	}

	// Look up translated username
	displayName := username
	user, err := getUserByUsername(username)
	if err == nil {
		displayName = getUserText(user.ID, lang)
	}

	// Look up translated reason
	displayReason := getReasonText(reasonID, reasonText, lang)

	absStars := stars
	if absStars < 0 {
		absStars = -absStars
	}

	message := formatAnnounceMessage(lang, displayName, displayReason, stars, absStars)

	go func() {
		payload := map[string]interface{}{
			"entity_id": haEntity,
			"message":   message,
		}
		body, _ := json.Marshal(payload)

		url := strings.TrimRight(haURL, "/")
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

func formatAnnounceMessage(lang, name, reason string, stars, absStars int) string {
	switch lang {
	case "zh-CN":
		if stars > 0 {
			return fmt.Sprintf("%s 因为%s获得了 %d 颗星星！", name, reason, absStars)
		}
		return fmt.Sprintf("%s 因为%s失去了 %d 颗星星！", name, reason, absStars)
	case "zh-TW":
		if stars > 0 {
			return fmt.Sprintf("%s 因為%s獲得了 %d 顆星星！", name, reason, absStars)
		}
		return fmt.Sprintf("%s 因為%s失去了 %d 顆星星！", name, reason, absStars)
	default: // en
		if stars > 0 {
			return fmt.Sprintf("%s got %d stars for %s!", name, absStars, reason)
		}
		return fmt.Sprintf("%s lost %d stars for %s!", name, absStars, reason)
	}
}
