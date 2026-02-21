package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
)

// sendHAAnnouncement sends a TTS message via Home Assistant.
func sendHAAnnouncement(message string) {
	haURL := getSetting("ha_url")
	haToken := getSetting("ha_token")
	haEntity := getSetting("ha_media_player")

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

func haEnabled() bool {
	if getSetting("ha_enabled") != "1" {
		return false
	}
	haURL := getSetting("ha_url")
	haToken := getSetting("ha_token")
	haEntity := getSetting("ha_media_player")
	return haURL != "" && haToken != "" && haEntity != ""
}

func announceStarIfEnabled(username string, reasonID *int, reasonText string, stars int) {
	if !haEnabled() {
		return
	}

	lang := getSetting("ha_lang")
	if lang == "" {
		lang = "en"
	}

	displayName := username
	user, err := getUserByUsername(username)
	if err == nil {
		displayName = getUserText(user.ID, lang)
	}

	displayReason := getReasonText(reasonID, reasonText, lang)

	absStars := stars
	if absStars < 0 {
		absStars = -absStars
	}

	message := formatAnnounceMessage(lang, displayName, displayReason, stars, absStars)
	sendHAAnnouncement(message)
}

func announceRedemptionIfEnabled(username string, rewardID int, isAdmin bool) {
	if isAdmin {
		return
	}
	if !haEnabled() {
		return
	}

	lang := getSetting("ha_lang")
	if lang == "" {
		lang = "en"
	}

	displayName := username
	user, err := getUserByUsername(username)
	if err == nil {
		displayName = getUserText(user.ID, lang)
	}

	displayReward := getRewardText(rewardID, lang)

	message := formatRedemptionMessage(lang, displayName, displayReward)
	sendHAAnnouncement(message)
}

var numberWords = map[string][]string{
	"en": {"zero", "one", "two", "three", "four", "five", "six", "seven",
		"eight", "nine", "ten", "eleven", "twelve", "thirteen", "fourteen",
		"fifteen", "sixteen", "seventeen", "eighteen", "nineteen", "twenty"},
	"zh-CN": {"零", "一", "二", "三", "四", "五", "六", "七", "八", "九",
		"十", "十一", "十二", "十三", "十四", "十五", "十六", "十七", "十八", "十九", "二十"},
	"zh-TW": {"零", "一", "二", "三", "四", "五", "六", "七", "八", "九",
		"十", "十一", "十二", "十三", "十四", "十五", "十六", "十七", "十八", "十九", "二十"},
}

func numWord(n int, lang string) string {
	words, ok := numberWords[lang]
	if ok && n >= 0 && n < len(words) {
		return words[n]
	}
	return fmt.Sprintf("%d", n)
}

func formatAnnounceMessage(lang, name, reason string, stars, absStars int) string {
	n := numWord(absStars, lang)
	switch lang {
	case "zh-CN":
		if stars > 0 {
			return fmt.Sprintf("%s因为%s获得了%s颗星星！", name, reason, n)
		}
		return fmt.Sprintf("%s因为%s失去了%s颗星星！", name, reason, n)
	case "zh-TW":
		if stars > 0 {
			return fmt.Sprintf("%s因為%s獲得了%s顆星星！", name, reason, n)
		}
		return fmt.Sprintf("%s因為%s失去了%s顆星星！", name, reason, n)
	default: // en
		if stars > 0 {
			return fmt.Sprintf("%s got %s stars for %s!", name, n, reason)
		}
		return fmt.Sprintf("%s lost %s stars for %s!", name, n, reason)
	}
}

func formatRedemptionMessage(lang, name, reward string) string {
	switch lang {
	case "zh-CN":
		return fmt.Sprintf("%s 兑换了%s！", name, reward)
	case "zh-TW":
		return fmt.Sprintf("%s 兌換了%s！", name, reward)
	default:
		return fmt.Sprintf("%s redeemed %s!", name, reward)
	}
}
