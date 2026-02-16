package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
	"golang.org/x/crypto/bcrypt"
)

var db *sql.DB

func initDB(dbPath string) error {
	var err error
	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return err
	}

	_, err = db.Exec("PRAGMA journal_mode=WAL")
	if err != nil {
		return fmt.Errorf("failed to set WAL mode: %w", err)
	}
	_, err = db.Exec("PRAGMA foreign_keys=ON")
	if err != nil {
		return fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY,
		username TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		is_admin BOOLEAN DEFAULT FALSE
	);
	CREATE TABLE IF NOT EXISTS user_translations (
		id INTEGER PRIMARY KEY,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		lang TEXT NOT NULL,
		text TEXT NOT NULL,
		UNIQUE(user_id, lang)
	);
	CREATE TABLE IF NOT EXISTS reasons (
		id INTEGER PRIMARY KEY,
		key TEXT UNIQUE NOT NULL,
		stars INTEGER NOT NULL DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS reason_translations (
		id INTEGER PRIMARY KEY,
		reason_id INTEGER NOT NULL REFERENCES reasons(id) ON DELETE CASCADE,
		lang TEXT NOT NULL,
		text TEXT NOT NULL,
		UNIQUE(reason_id, lang)
	);
	CREATE TABLE IF NOT EXISTS stars (
		id INTEGER PRIMARY KEY,
		user_id INTEGER NOT NULL REFERENCES users(id),
		reason_id INTEGER REFERENCES reasons(id),
		reason_text TEXT,
		stars INTEGER NOT NULL DEFAULT 1,
		awarded_by INTEGER REFERENCES users(id),
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS api_keys (
		id INTEGER PRIMARY KEY,
		key_hash TEXT UNIQUE NOT NULL,
		label TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS sessions (
		token TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL REFERENCES users(id),
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS rewards (
		id INTEGER PRIMARY KEY,
		key TEXT UNIQUE NOT NULL,
		cost INTEGER NOT NULL,
		icon TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS reward_translations (
		id INTEGER PRIMARY KEY,
		reward_id INTEGER NOT NULL REFERENCES rewards(id) ON DELETE CASCADE,
		lang TEXT NOT NULL,
		text TEXT NOT NULL,
		UNIQUE(reward_id, lang)
	);
	CREATE TABLE IF NOT EXISTS redemptions (
		id INTEGER PRIMARY KEY,
		user_id INTEGER NOT NULL REFERENCES users(id),
		reward_id INTEGER NOT NULL REFERENCES rewards(id),
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL DEFAULT ''
	);`

	_, err = db.Exec(schema)
	if err != nil {
		return err
	}

	// Run migrations
	return runMigrations()
}

func runMigrations() error {
	// Add stars column to reasons table if it doesn't exist
	var columnExists bool
	err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('reasons') WHERE name='stars'").Scan(&columnExists)
	if err == nil && !columnExists {
		_, err = db.Exec("ALTER TABLE reasons ADD COLUMN stars INTEGER NOT NULL DEFAULT 1")
		if err != nil {
			return fmt.Errorf("failed to add stars column: %w", err)
		}
	}

	// Create user_translations table if it doesn't exist
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS user_translations (
		id INTEGER PRIMARY KEY,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		lang TEXT NOT NULL,
		text TEXT NOT NULL,
		UNIQUE(user_id, lang)
	)`)
	if err != nil {
		return fmt.Errorf("failed to create user_translations table: %w", err)
	}

	// Migrate rewards table to use key + translations
	var keyExists bool
	err = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('rewards') WHERE name='key'").Scan(&keyExists)
	if err == nil && !keyExists {
		// Create reward_translations table
		_, err = db.Exec(`CREATE TABLE IF NOT EXISTS reward_translations (
			id INTEGER PRIMARY KEY,
			reward_id INTEGER NOT NULL REFERENCES rewards(id) ON DELETE CASCADE,
			lang TEXT NOT NULL,
			text TEXT NOT NULL,
			UNIQUE(reward_id, lang)
		)`)
		if err != nil {
			return fmt.Errorf("failed to create reward_translations table: %w", err)
		}

		// Add key column to rewards
		_, err = db.Exec("ALTER TABLE rewards ADD COLUMN key TEXT")
		if err != nil {
			return fmt.Errorf("failed to add key column to rewards: %w", err)
		}

		// Migrate existing rewards: copy name to key and create English translation
		rows, err := db.Query("SELECT id, name FROM rewards WHERE key IS NULL")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id int
				var name string
				rows.Scan(&id, &name)
				key := sanitizeKey(name)
				db.Exec("UPDATE rewards SET key = ? WHERE id = ?", key, id)
				db.Exec("INSERT INTO reward_translations (reward_id, lang, text) VALUES (?, 'en', ?)", id, name)
			}
		}
	}

	return nil
}

func seedUsers() error {
	var count int
	db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if count > 0 {
		return nil
	}

	users := []struct {
		username string
		password string
		isAdmin  bool
	}{
		{"dad", "changeme", true},
		{"mom", "changeme", true},
		{"theo", "changeme", false},
		{"ray", "changeme", false},
	}

	for _, u := range users {
		hash, err := bcrypt.GenerateFromPassword([]byte(u.password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		_, err = db.Exec("INSERT INTO users (username, password_hash, is_admin) VALUES (?, ?, ?)",
			u.username, string(hash), u.isAdmin)
		if err != nil {
			return err
		}
	}
	fmt.Println("Seeded default users (password: changeme): dad, mom, theo, ray")
	return nil
}

func seedRewards() error {
	var count int
	db.QueryRow("SELECT COUNT(*) FROM rewards").Scan(&count)
	if count > 0 {
		return nil
	}

	rewards := []struct {
		key  string
		name string
		cost int
		icon string
	}{
		{"extra_screen_time", "Extra screen time", 5, "📱"},
		{"choose_dinner", "Choose dinner", 6, "🍽️"},
		{"stay_up_late", "Stay up late", 7, "🌙"},
		{"ice_cream_outing", "Ice cream outing", 8, "🍦"},
		{"movie_time", "Movie time", 10, "🎬"},
		{"day_trip_choice", "Day trip choice", 15, "🚗"},
	}

	for _, r := range rewards {
		result, err := db.Exec("INSERT INTO rewards (key, cost, icon) VALUES (?, ?, ?)", r.key, r.cost, r.icon)
		if err != nil {
			return err
		}
		id, _ := result.LastInsertId()
		// Add English translation
		db.Exec("INSERT INTO reward_translations (reward_id, lang, text) VALUES (?, 'en', ?)", id, r.name)
	}
	fmt.Println("Seeded default rewards")
	return nil
}

func updatePassword(userID int, newHash string) error {
	_, err := db.Exec("UPDATE users SET password_hash = ? WHERE id = ?", newHash, userID)
	return err
}

func getUserByUsername(username string) (*User, error) {
	u := &User{}
	err := db.QueryRow("SELECT id, username, password_hash, is_admin FROM users WHERE username = ?", username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsAdmin)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func getUserByID(id int) (*User, error) {
	u := &User{}
	err := db.QueryRow("SELECT id, username, password_hash, is_admin FROM users WHERE id = ?", id).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsAdmin)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func getAllUsers() ([]User, error) {
	rows, err := db.Query("SELECT id, username, password_hash, is_admin FROM users")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		u.Translations = make(map[string]string)
		rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsAdmin)

		// Load all translations for this user
		tRows, _ := db.Query("SELECT lang, text FROM user_translations WHERE user_id = ?", u.ID)
		for tRows.Next() {
			var lang, text string
			tRows.Scan(&lang, &text)
			u.Translations[lang] = text
		}
		tRows.Close()

		users = append(users, u)
	}
	return users, nil
}

func updateUserTranslation(userID int, lang, text string) error {
	_, err := db.Exec(`
		INSERT INTO user_translations (user_id, lang, text) VALUES (?, ?, ?)
		ON CONFLICT(user_id, lang) DO UPDATE SET text = ?
	`, userID, lang, text, text)
	return err
}

func getUserText(userID int, lang string) string {
	var text string
	// Try to get translation in requested language
	err := db.QueryRow("SELECT text FROM user_translations WHERE user_id = ? AND lang = ?", userID, lang).Scan(&text)
	if err == nil && text != "" {
		return text
	}
	// Fallback to English
	err = db.QueryRow("SELECT text FROM user_translations WHERE user_id = ? AND lang = 'en'", userID).Scan(&text)
	if err == nil && text != "" {
		return text
	}
	// Fallback to username
	var username string
	db.QueryRow("SELECT username FROM users WHERE id = ?", userID).Scan(&username)
	return username
}

type UserStarCount struct {
	Username     string
	DisplayNameEN string
	DisplayNameCN string
	DisplayNameTW string
	StarCount    int
	CurrentStars int
	IsAdmin      bool
}

func getUserStarCounts() ([]UserStarCount, error) {
	rows, err := db.Query(`
		SELECT u.id, u.username, u.is_admin, COALESCE(SUM(s.stars), 0) as star_count,
			COALESCE(SUM(s.stars), 0) - COALESCE((SELECT SUM(rw.cost) FROM redemptions rd JOIN rewards rw ON rd.reward_id = rw.id WHERE rd.user_id = u.id), 0) as current_stars
		FROM users u LEFT JOIN stars s ON u.id = s.user_id
		GROUP BY u.id ORDER BY star_count DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []UserStarCount
	for rows.Next() {
		var r UserStarCount
		var userID int
		rows.Scan(&userID, &r.Username, &r.IsAdmin, &r.StarCount, &r.CurrentStars)

		// Get all translations for this user
		r.DisplayNameEN = getUserText(userID, "en")
		r.DisplayNameCN = getUserText(userID, "zh-CN")
		r.DisplayNameTW = getUserText(userID, "zh-TW")

		results = append(results, r)
	}
	return results, nil
}

func getStars(filterUsername string) ([]Star, error) {
	query := `SELECT s.id, s.user_id, u.username, s.reason_id, s.reason_text, s.stars, s.awarded_by, COALESCE(a.username,''), s.created_at
		FROM stars s
		JOIN users u ON s.user_id = u.id
		LEFT JOIN users a ON s.awarded_by = a.id`
	var args []interface{}
	if filterUsername != "" {
		query += " WHERE u.username = ?"
		args = append(args, filterUsername)
	}
	query += " ORDER BY s.created_at DESC"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stars []Star
	for rows.Next() {
		var s Star
		var reasonText sql.NullString
		var createdAtStr sql.NullString
		err := rows.Scan(&s.ID, &s.UserID, &s.Username, &s.ReasonID, &reasonText, &s.Stars, &s.AwardedBy, &s.AwardedByName, &createdAtStr)
		if err != nil {
			fmt.Printf("Error scanning star row: %v\n", err)
			continue
		}

		// Handle NULL reason_text
		if reasonText.Valid {
			s.ReasonText = reasonText.String
		}

		// Parse the datetime string - modernc.org/sqlite returns RFC3339 format
		if createdAtStr.Valid && createdAtStr.String != "" {
			// Try RFC3339 first (what modernc.org/sqlite returns)
			if t, err := time.Parse(time.RFC3339, createdAtStr.String); err == nil {
				s.CreatedAt = t
			} else if t, err := time.Parse("2006-01-02 15:04:05", createdAtStr.String); err == nil {
				s.CreatedAt = t
			}
		}
		stars = append(stars, s)
	}
	return stars, nil
}

func addStar(username, reason string, awardedBy int) error {
	_, err := addStarWithID(username, nil, reason, 1, awardedBy)
	return err
}

func addStarWithID(username string, reasonID *int, reasonText string, stars int, awardedBy int) (int64, error) {
	user, err := getUserByUsername(username)
	if err != nil {
		return 0, fmt.Errorf("user not found: %s", username)
	}

	// If reason ID provided, use it directly
	if reasonID != nil && *reasonID > 0 {
		// Get the star count from the reason if not explicitly provided
		if stars < 1 {
			var reasonStars int
			err := db.QueryRow("SELECT stars FROM reasons WHERE id = ?", reasonID).Scan(&reasonStars)
			if err == nil && reasonStars > 0 {
				stars = reasonStars
			} else {
				stars = 1
			}
		}
		result, err := db.Exec("INSERT INTO stars (user_id, reason_id, stars, awarded_by) VALUES (?, ?, ?, ?)",
			user.ID, reasonID, stars, awardedBy)
		if err != nil {
			return 0, err
		}
		return result.LastInsertId()
	}

	// Ensure stars is at least 1 for custom reasons
	if stars < 1 {
		stars = 1
	}

	// Otherwise, create a new reason from text
	if reasonText == "" {
		return 0, fmt.Errorf("reason required")
	}

	// Try to find existing reason by matching English translation
	var existingID *int
	err = db.QueryRow("SELECT r.id FROM reasons r JOIN reason_translations rt ON r.id = rt.reason_id WHERE rt.text = ? AND rt.lang = 'en'", reasonText).Scan(&existingID)

	if err == nil && existingID != nil {
		// Found existing reason, use it
		reasonID = existingID
	} else {
		// Create new reason with specified star count
		key := sanitizeKey(reasonText)
		result, err := db.Exec("INSERT INTO reasons (key, stars) VALUES (?, ?)", key, stars)
		if err != nil {
			return 0, err
		}
		id, _ := result.LastInsertId()
		rid := int(id)
		reasonID = &rid

		// Add English translation
		db.Exec("INSERT INTO reason_translations (reason_id, lang, text) VALUES (?, 'en', ?)", rid, reasonText)
	}

	result, err := db.Exec("INSERT INTO stars (user_id, reason_id, stars, awarded_by) VALUES (?, ?, ?, ?)",
		user.ID, reasonID, stars, awardedBy)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func sanitizeKey(text string) string {
	// Simple key generation from text
	key := ""
	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			key += string(r)
		} else if r == ' ' {
			key += "_"
		}
	}
	if key == "" {
		key = "custom"
	}
	// Add timestamp to ensure uniqueness
	return key
}

func updateReasonTranslation(reasonID int, lang, text string) error {
	_, err := db.Exec(`
		INSERT INTO reason_translations (reason_id, lang, text) VALUES (?, ?, ?)
		ON CONFLICT(reason_id, lang) DO UPDATE SET text = ?
	`, reasonID, lang, text, text)
	return err
}

func updateReasonStars(reasonID int, stars int) error {
	if stars < 1 {
		stars = 1
	}
	// Update the reason's default star count
	_, err := db.Exec("UPDATE reasons SET stars = ? WHERE id = ?", stars, reasonID)
	if err != nil {
		return err
	}
	// Retroactively update all existing stars with this reason
	_, err = db.Exec("UPDATE stars SET stars = ? WHERE reason_id = ?", stars, reasonID)
	return err
}

func deleteReason(reasonID int) error {
	_, err := db.Exec("DELETE FROM reasons WHERE id = ?", reasonID)
	return err
}

func deleteStar(id int) error {
	_, err := db.Exec("DELETE FROM stars WHERE id = ?", id)
	return err
}

func getReasons() ([]Reason, error) {
	// Get reasons with star count
	rows, err := db.Query(`
		SELECT r.id, r.key, r.stars, COUNT(s.id) as count
		FROM reasons r
		LEFT JOIN stars s ON r.id = s.reason_id
		GROUP BY r.id, r.key, r.stars
		ORDER BY count DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reasons []Reason
	for rows.Next() {
		var r Reason
		r.Translations = make(map[string]string)
		rows.Scan(&r.ID, &r.Key, &r.Stars, &r.Count)

		// Load all translations for this reason
		tRows, _ := db.Query("SELECT lang, text FROM reason_translations WHERE reason_id = ?", r.ID)
		for tRows.Next() {
			var lang, text string
			tRows.Scan(&lang, &text)
			r.Translations[lang] = text
		}
		tRows.Close()

		reasons = append(reasons, r)
	}
	return reasons, nil
}

func getReasonText(reasonID *int, reasonText string, lang string) string {
	if reasonID != nil && *reasonID > 0 {
		var text string
		// Try to get translation in requested language
		err := db.QueryRow("SELECT text FROM reason_translations WHERE reason_id = ? AND lang = ?", reasonID, lang).Scan(&text)
		if err == nil && text != "" {
			return text
		}
		// Fallback to English
		err = db.QueryRow("SELECT text FROM reason_translations WHERE reason_id = ? AND lang = 'en'", reasonID).Scan(&text)
		if err == nil && text != "" {
			return text
		}
	}
	return reasonText
}

func hashAPIKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

func addAPIKey(keyHash, label string) error {
	_, err := db.Exec("INSERT INTO api_keys (key_hash, label) VALUES (?, ?)", keyHash, label)
	return err
}

func getAPIKeys() ([]APIKey, error) {
	rows, err := db.Query("SELECT id, key_hash, label, created_at FROM api_keys")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []APIKey
	for rows.Next() {
		var k APIKey
		rows.Scan(&k.ID, &k.KeyHash, &k.Label, &k.CreatedAt)
		keys = append(keys, k)
	}
	return keys, nil
}

func deleteAPIKey(id int) error {
	_, err := db.Exec("DELETE FROM api_keys WHERE id = ?", id)
	return err
}

func validateAPIKey(key string) bool {
	h := hashAPIKey(key)
	var count int
	db.QueryRow("SELECT COUNT(*) FROM api_keys WHERE key_hash = ?", h).Scan(&count)
	return count > 0
}

// Session management using DB
func createSession(token string, userID int) error {
	_, err := db.Exec("INSERT INTO sessions (token, user_id) VALUES (?, ?)", token, userID)
	return err
}

func getSession(token string) (int, error) {
	var userID int
	err := db.QueryRow("SELECT user_id FROM sessions WHERE token = ?", token).Scan(&userID)
	return userID, err
}

func deleteSession(token string) error {
	_, err := db.Exec("DELETE FROM sessions WHERE token = ?", token)
	return err
}

func getRewardsList() ([]Reward, error) {
	rows, err := db.Query("SELECT id, cost, icon FROM rewards ORDER BY cost ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rewards []Reward
	for rows.Next() {
		var r Reward
		r.Translations = make(map[string]string)
		rows.Scan(&r.ID, &r.Cost, &r.Icon)

		// Load all translations for this reward
		tRows, _ := db.Query("SELECT lang, text FROM reward_translations WHERE reward_id = ?", r.ID)
		for tRows.Next() {
			var lang, text string
			tRows.Scan(&lang, &text)
			r.Translations[lang] = text
			if lang == "en" {
				r.Name = text // Keep Name field for backward compatibility
			}
		}
		tRows.Close()

		rewards = append(rewards, r)
	}
	return rewards, nil
}

func addReward(name string, cost int, icon string) error {
	key := sanitizeKey(name)
	result, err := db.Exec("INSERT INTO rewards (key, cost, icon) VALUES (?, ?, ?)", key, cost, icon)
	if err != nil {
		return err
	}
	id, _ := result.LastInsertId()
	// Add English translation
	_, err = db.Exec("INSERT INTO reward_translations (reward_id, lang, text) VALUES (?, 'en', ?)", id, name)
	return err
}

func updateReward(id int, name string, cost int, icon string) error {
	_, err := db.Exec("UPDATE rewards SET cost = ?, icon = ? WHERE id = ?", cost, icon, id)
	if err != nil {
		return err
	}
	// Update English translation
	_, err = db.Exec(`
		INSERT INTO reward_translations (reward_id, lang, text) VALUES (?, 'en', ?)
		ON CONFLICT(reward_id, lang) DO UPDATE SET text = ?
	`, id, name, name)
	return err
}

func updateRewardTranslation(rewardID int, lang, text string) error {
	_, err := db.Exec(`
		INSERT INTO reward_translations (reward_id, lang, text) VALUES (?, ?, ?)
		ON CONFLICT(reward_id, lang) DO UPDATE SET text = ?
	`, rewardID, lang, text, text)
	return err
}

func updateRewardCost(rewardID int, cost int) error {
	if cost < 1 {
		cost = 1
	}
	_, err := db.Exec("UPDATE rewards SET cost = ? WHERE id = ?", cost, rewardID)
	return err
}

func getRewardText(rewardID int, lang string) string {
	var text string
	// Try to get translation in requested language
	err := db.QueryRow("SELECT text FROM reward_translations WHERE reward_id = ? AND lang = ?", rewardID, lang).Scan(&text)
	if err == nil && text != "" {
		return text
	}
	// Fallback to English
	err = db.QueryRow("SELECT text FROM reward_translations WHERE reward_id = ? AND lang = 'en'", rewardID).Scan(&text)
	if err == nil && text != "" {
		return text
	}
	return ""
}

func deleteRewardByID(id int) error {
	_, err := db.Exec("DELETE FROM rewards WHERE id = ?", id)
	return err
}

func getRewardByID(id int) (*Reward, error) {
	r := &Reward{}
	r.Translations = make(map[string]string)
	err := db.QueryRow("SELECT id, cost, icon FROM rewards WHERE id = ?", id).
		Scan(&r.ID, &r.Cost, &r.Icon)
	if err != nil {
		return nil, err
	}

	// Load all translations
	rows, _ := db.Query("SELECT lang, text FROM reward_translations WHERE reward_id = ?", id)
	defer rows.Close()
	for rows.Next() {
		var lang, text string
		rows.Scan(&lang, &text)
		r.Translations[lang] = text
		if lang == "en" {
			r.Name = text
		}
	}

	return r, nil
}

func getUserCurrentStars(userID int) (int, error) {
	var current int
	err := db.QueryRow(`
		SELECT COALESCE(SUM(s.stars), 0) - COALESCE((SELECT SUM(rw.cost) FROM redemptions rd JOIN rewards rw ON rd.reward_id = rw.id WHERE rd.user_id = ?), 0)
		FROM stars s WHERE s.user_id = ?`, userID, userID).Scan(&current)
	return current, err
}

func redeemReward(userID, rewardID int) error {
	_, err := db.Exec("INSERT INTO redemptions (user_id, reward_id) VALUES (?, ?)", userID, rewardID)
	return err
}

func getSetting(key string) string {
	var val string
	db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&val)
	return val
}

func setSetting(key, value string) error {
	_, err := db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = ?`, key, value, value)
	return err
}

func getRecentRedemptions(limit int, filterUserID int) ([]Redemption, error) {
	query := `SELECT rd.id, rd.user_id, u.username, rd.reward_id, rw.cost, rd.created_at
		FROM redemptions rd
		JOIN users u ON rd.user_id = u.id
		JOIN rewards rw ON rd.reward_id = rw.id`
	var args []interface{}
	if filterUserID > 0 {
		query += " WHERE rd.user_id = ?"
		args = append(args, filterUserID)
	}
	query += " ORDER BY rd.created_at DESC LIMIT ?"
	args = append(args, limit)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []Redemption
	for rows.Next() {
		var r Redemption
		var rewardID int
		var createdAtStr sql.NullString
		err := rows.Scan(&r.ID, &r.UserID, &r.Username, &rewardID, &r.Cost, &createdAtStr)
		if err != nil {
			fmt.Printf("Error scanning redemption row: %v\n", err)
			continue
		}

		// Get all translations for this reward
		r.RewardNameEN = getRewardText(rewardID, "en")
		r.RewardNameCN = getRewardText(rewardID, "zh-CN")
		r.RewardNameTW = getRewardText(rewardID, "zh-TW")
		r.RewardName = r.RewardNameEN // Keep for backward compatibility

		// Parse the datetime string - modernc.org/sqlite returns RFC3339 format
		if createdAtStr.Valid && createdAtStr.String != "" {
			// Try RFC3339 first (what modernc.org/sqlite returns)
			if t, err := time.Parse(time.RFC3339, createdAtStr.String); err == nil {
				r.CreatedAt = t
			} else if t, err := time.Parse("2006-01-02 15:04:05", createdAtStr.String); err == nil {
				r.CreatedAt = t
			}
		}
		results = append(results, r)
	}
	return results, nil
}
