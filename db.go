package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
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

func columnExists(table, column string) bool {
	var exists bool
	db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('"+table+"') WHERE name=?", column).Scan(&exists)
	return exists
}

func runMigrations() error {
	// --- stars table migrations ---
	// Add reason_id column (originally stars had "reason TEXT NOT NULL")
	if !columnExists("stars", "reason_id") {
		if _, err := db.Exec("ALTER TABLE stars ADD COLUMN reason_id INTEGER REFERENCES reasons(id)"); err != nil {
			return fmt.Errorf("failed to add reason_id column to stars: %w", err)
		}
	}
	// Add reason_text column
	if !columnExists("stars", "reason_text") {
		if _, err := db.Exec("ALTER TABLE stars ADD COLUMN reason_text TEXT"); err != nil {
			return fmt.Errorf("failed to add reason_text column to stars: %w", err)
		}
		// Migrate data from old "reason" column if it exists
		if columnExists("stars", "reason") {
			db.Exec("UPDATE stars SET reason_text = reason WHERE reason_text IS NULL")
		}
	}
	// Add stars (count per record) column
	if !columnExists("stars", "stars") {
		if _, err := db.Exec("ALTER TABLE stars ADD COLUMN stars INTEGER NOT NULL DEFAULT 1"); err != nil {
			return fmt.Errorf("failed to add stars column to stars: %w", err)
		}
	}

	// --- reasons table migrations ---
	// Add key column (originally reasons had "text TEXT UNIQUE NOT NULL")
	if !columnExists("reasons", "key") {
		if _, err := db.Exec("ALTER TABLE reasons ADD COLUMN key TEXT"); err != nil {
			return fmt.Errorf("failed to add key column to reasons: %w", err)
		}
		// Migrate data from old "text" column if it exists
		if columnExists("reasons", "text") {
			rows, err := db.Query("SELECT id, text FROM reasons WHERE key IS NULL")
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var id int
					var text string
					rows.Scan(&id, &text)
					db.Exec("UPDATE reasons SET key = ? WHERE id = ?", sanitizeKey(text), id)
				}
			}
		}
	}
	// Add stars column to reasons
	if !columnExists("reasons", "stars") {
		if _, err := db.Exec("ALTER TABLE reasons ADD COLUMN stars INTEGER NOT NULL DEFAULT 1"); err != nil {
			return fmt.Errorf("failed to add stars column to reasons: %w", err)
		}
	}
	// Add created_at column to reasons
	if !columnExists("reasons", "created_at") {
		if _, err := db.Exec("ALTER TABLE reasons ADD COLUMN created_at DATETIME DEFAULT CURRENT_TIMESTAMP"); err != nil {
			return fmt.Errorf("failed to add created_at column to reasons: %w", err)
		}
	}

	// --- reason_translations table ---
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS reason_translations (
		id INTEGER PRIMARY KEY,
		reason_id INTEGER NOT NULL REFERENCES reasons(id) ON DELETE CASCADE,
		lang TEXT NOT NULL,
		text TEXT NOT NULL,
		UNIQUE(reason_id, lang)
	)`)
	if err != nil {
		return fmt.Errorf("failed to create reason_translations table: %w", err)
	}
	// Migrate old reason text to English translations if not already done
	if columnExists("reasons", "text") {
		rows, err := db.Query(`SELECT r.id, r.text FROM reasons r
			WHERE r.text IS NOT NULL AND r.text != ''
			AND NOT EXISTS (SELECT 1 FROM reason_translations rt WHERE rt.reason_id = r.id AND rt.lang = 'en')`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id int
				var text string
				rows.Scan(&id, &text)
				db.Exec("INSERT OR IGNORE INTO reason_translations (reason_id, lang, text) VALUES (?, 'en', ?)", id, text)
			}
		}
	}

	// --- user_translations table ---
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

	// --- rewards table migrations ---
	// Add icon column
	if !columnExists("rewards", "icon") {
		if _, err := db.Exec("ALTER TABLE rewards ADD COLUMN icon TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("failed to add icon column to rewards: %w", err)
		}
	}
	// Add key column (originally rewards had "name TEXT UNIQUE NOT NULL")
	if !columnExists("rewards", "key") {
		if _, err := db.Exec("ALTER TABLE rewards ADD COLUMN key TEXT"); err != nil {
			return fmt.Errorf("failed to add key column to rewards: %w", err)
		}
		// Migrate data from old "name" column if it exists
		if columnExists("rewards", "name") {
			rows, err := db.Query("SELECT id, name FROM rewards WHERE key IS NULL")
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var id int
					var name string
					rows.Scan(&id, &name)
					db.Exec("UPDATE rewards SET key = ? WHERE id = ?", sanitizeKey(name), id)
				}
			}
		}
	}

	// --- reward_translations table ---
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
	// Migrate old reward name to English translations if not already done
	if columnExists("rewards", "name") {
		rows, err := db.Query(`SELECT r.id, r.name FROM rewards r
			WHERE r.name IS NOT NULL AND r.name != ''
			AND NOT EXISTS (SELECT 1 FROM reward_translations rt WHERE rt.reward_id = r.id AND rt.lang = 'en')`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id int
				var name string
				rows.Scan(&id, &name)
				db.Exec("INSERT OR IGNORE INTO reward_translations (reward_id, lang, text) VALUES (?, 'en', ?)", id, name)
			}
		}
	}

	// Add adult_only column to rewards
	if !columnExists("rewards", "adult_only") {
		if _, err := db.Exec("ALTER TABLE rewards ADD COLUMN adult_only BOOLEAN DEFAULT FALSE"); err != nil {
			return fmt.Errorf("failed to add adult_only column to rewards: %w", err)
		}
	}

	// --- redemptions table migrations ---
	// Add cost column to snapshot cost at redemption time
	if !columnExists("redemptions", "cost") {
		if _, err := db.Exec("ALTER TABLE redemptions ADD COLUMN cost INTEGER"); err != nil {
			return fmt.Errorf("failed to add cost column to redemptions: %w", err)
		}
	}

	// --- settings table ---
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		return fmt.Errorf("failed to create settings table: %w", err)
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
		isAdmin  bool
	}{
		{"dad", true},
		{"mom", true},
		{"theo", false},
		{"ray", false},
	}

	defaultPassword := strings.TrimSpace(os.Getenv("STAR_APP_DEFAULT_PASSWORD"))
	credentials := make(map[string]string, len(users))

	for _, u := range users {
		password := defaultPassword
		if password == "" {
			generated, err := randomHex(10)
			if err != nil {
				return fmt.Errorf("failed to generate seed password: %w", err)
			}
			password = generated
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		_, err = db.Exec("INSERT INTO users (username, password_hash, is_admin) VALUES (?, ?, ?)",
			u.username, string(hash), u.isAdmin)
		if err != nil {
			return err
		}
		credentials[u.username] = password
	}

	names := make([]string, 0, len(credentials))
	for username := range credentials {
		names = append(names, username)
	}
	sort.Strings(names)

	fmt.Println("Seeded default users with temporary passwords:")
	for _, username := range names {
		fmt.Printf("  %s: %s\n", username, credentials[username])
	}
	fmt.Println("Change these passwords immediately from Account settings.")

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

func addUser(username, password string, isAdmin bool) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = db.Exec("INSERT INTO users (username, password_hash, is_admin) VALUES (?, ?, ?)",
		username, string(hash), isAdmin)
	return err
}

func deleteUser(id int) error {
	db.Exec("DELETE FROM sessions WHERE user_id = ?", id)
	db.Exec("DELETE FROM user_translations WHERE user_id = ?", id)
	db.Exec("DELETE FROM redemptions WHERE user_id = ?", id)
	db.Exec("DELETE FROM stars WHERE user_id = ?", id)
	_, err := db.Exec("DELETE FROM users WHERE id = ?", id)
	return err
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
	u.Translations = make(map[string]string)
	err := db.QueryRow("SELECT id, username, password_hash, is_admin FROM users WHERE id = ?", id).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsAdmin)
	if err != nil {
		return nil, err
	}

	// Load all translations for this user
	tRows, _ := db.Query("SELECT lang, text FROM user_translations WHERE user_id = ?", u.ID)
	defer tRows.Close()
	for tRows.Next() {
		var lang, text string
		tRows.Scan(&lang, &text)
		u.Translations[lang] = text
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
	UserID        int
	Username      string
	DisplayNameEN string
	DisplayNameCN string
	DisplayNameTW string
	StarCount     int
	CurrentStars  int
	IsAdmin       bool
}

func getUserStarCounts() ([]UserStarCount, error) {
	rows, err := db.Query(`
		SELECT u.id, u.username, u.is_admin, COALESCE(SUM(s.stars), 0) as star_count,
			COALESCE(SUM(s.stars), 0) - COALESCE((SELECT SUM(COALESCE(rd.cost, rw.cost)) FROM redemptions rd JOIN rewards rw ON rd.reward_id = rw.id WHERE rd.user_id = u.id), 0) as current_stars
		FROM users u LEFT JOIN stars s ON u.id = s.user_id
		GROUP BY u.id ORDER BY star_count DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []UserStarCount
	for rows.Next() {
		var r UserStarCount
		rows.Scan(&r.UserID, &r.Username, &r.IsAdmin, &r.StarCount, &r.CurrentStars)

		// Get all translations for this user
		r.DisplayNameEN = getUserText(r.UserID, "en")
		r.DisplayNameCN = getUserText(r.UserID, "zh-CN")
		r.DisplayNameTW = getUserText(r.UserID, "zh-TW")

		results = append(results, r)
	}
	return results, nil
}

// getUserReasonCounts returns map[userID]map[reasonID]count
func getUserReasonCounts() (map[int]map[int]int, error) {
	rows, err := db.Query(`SELECT user_id, reason_id, COUNT(*) FROM stars WHERE reason_id IS NOT NULL GROUP BY user_id, reason_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int]map[int]int)
	for rows.Next() {
		var userID, reasonID, count int
		rows.Scan(&userID, &reasonID, &count)
		if result[userID] == nil {
			result[userID] = make(map[int]int)
		}
		result[userID][reasonID] = count
	}
	return result, nil
}

func getStars(filterUsername string) ([]Star, error) {
	query := `SELECT s.id, s.user_id, u.username, s.reason_id, COALESCE(r.key, ''), s.reason_text, s.stars, s.awarded_by, COALESCE(a.username,''), s.created_at
		FROM stars s
		JOIN users u ON s.user_id = u.id
		LEFT JOIN reasons r ON s.reason_id = r.id
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
		var reasonKey sql.NullString
		var reasonText sql.NullString
		var createdAtStr sql.NullString
		err := rows.Scan(&s.ID, &s.UserID, &s.Username, &s.ReasonID, &reasonKey, &reasonText, &s.Stars, &s.AwardedBy, &s.AwardedByName, &createdAtStr)
		if err != nil {
			fmt.Printf("Error scanning star row: %v\n", err)
			continue
		}

		// Get user translations
		s.UsernameEN = getUserText(s.UserID, "en")
		s.UsernameCN = getUserText(s.UserID, "zh-CN")
		s.UsernameTW = getUserText(s.UserID, "zh-TW")

		// Get awarded_by user translations if awarded_by is set
		if s.AwardedBy > 0 {
			s.AwardedByNameEN = getUserText(s.AwardedBy, "en")
			s.AwardedByNameCN = getUserText(s.AwardedBy, "zh-CN")
			s.AwardedByNameTW = getUserText(s.AwardedBy, "zh-TW")
		}

		if reasonKey.Valid {
			s.ReasonKey = reasonKey.String
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
		if stars == 0 {
			var reasonStars int
			err := db.QueryRow("SELECT stars FROM reasons WHERE id = ?", reasonID).Scan(&reasonStars)
			if err == nil && reasonStars != 0 {
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

	// Default to 1 star for custom reasons if not specified
	if stars == 0 {
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
		key := uniqueKey(sanitizeKey(reasonText), "reasons", "key")
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

func updateReasonStars(reasonID int, stars int, retroactive bool) error {
	// Update the reason's default star count
	_, err := db.Exec("UPDATE reasons SET stars = ? WHERE id = ?", stars, reasonID)
	if err != nil {
		return err
	}
	if retroactive {
		// Retroactively update all existing stars with this reason
		_, err = db.Exec("UPDATE stars SET stars = ? WHERE reason_id = ?", stars, reasonID)
	}
	return err
}

func deleteReason(reasonID int) error {
	_, err := db.Exec("DELETE FROM reasons WHERE id = ?", reasonID)
	return err
}

func getStarByID(id int) (*Star, error) {
	var s Star
	var reasonText sql.NullString
	err := db.QueryRow("SELECT id, user_id, reason_id, reason_text, stars, awarded_by FROM stars WHERE id = ?", id).
		Scan(&s.ID, &s.UserID, &s.ReasonID, &reasonText, &s.Stars, &s.AwardedBy)
	if err != nil {
		return nil, err
	}
	if reasonText.Valid {
		s.ReasonText = reasonText.String
	}
	return &s, nil
}

func deleteStar(id int) error {
	_, err := db.Exec("DELETE FROM stars WHERE id = ?", id)
	return err
}

func getRedemptionByID(id int) (*Redemption, error) {
	var r Redemption
	err := db.QueryRow("SELECT id, user_id FROM redemptions WHERE id = ?", id).
		Scan(&r.ID, &r.UserID)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func deleteRedemption(id int) error {
	_, err := db.Exec("DELETE FROM redemptions WHERE id = ?", id)
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
	rows, err := db.Query("SELECT id, key, cost, icon, COALESCE(adult_only, 0) FROM rewards ORDER BY cost ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rewards []Reward
	for rows.Next() {
		var r Reward
		r.Translations = make(map[string]string)
		rows.Scan(&r.ID, &r.Key, &r.Cost, &r.Icon, &r.ForAdults)

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

func uniqueKey(baseKey, table, column string) string {
	key := baseKey
	for i := 2; ; i++ {
		var count int
		db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE "+column+" = ?", key).Scan(&count)
		if count == 0 {
			return key
		}
		key = fmt.Sprintf("%s_%d", baseKey, i)
	}
}

func addReward(name string, cost int, icon string, adultOnly bool) error {
	key := uniqueKey(sanitizeKey(name), "rewards", "key")
	var result sql.Result
	var err error
	// Migrated databases may still have the old "name" column with NOT NULL
	if columnExists("rewards", "name") {
		result, err = db.Exec("INSERT INTO rewards (name, key, cost, icon, adult_only) VALUES (?, ?, ?, ?, ?)", name, key, cost, icon, adultOnly)
	} else {
		result, err = db.Exec("INSERT INTO rewards (key, cost, icon, adult_only) VALUES (?, ?, ?, ?)", key, cost, icon, adultOnly)
	}
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

func updateRewardCost(rewardID int, cost int, retroactive bool) error {
	if cost < 1 {
		cost = 1
	}
	if !retroactive {
		// Snapshot current cost into existing redemptions before changing
		db.Exec("UPDATE redemptions SET cost = (SELECT cost FROM rewards WHERE id = ?) WHERE reward_id = ? AND cost IS NULL", rewardID, rewardID)
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

func updateRewardAdultOnly(rewardID int, adultOnly bool) error {
	_, err := db.Exec("UPDATE rewards SET adult_only = ? WHERE id = ?", adultOnly, rewardID)
	return err
}

func deleteRewardByID(id int) error {
	_, err := db.Exec("DELETE FROM rewards WHERE id = ?", id)
	return err
}

func getRewardByID(id int) (*Reward, error) {
	r := &Reward{}
	r.Translations = make(map[string]string)
	err := db.QueryRow("SELECT id, key, cost, icon, COALESCE(adult_only, 0) FROM rewards WHERE id = ?", id).
		Scan(&r.ID, &r.Key, &r.Cost, &r.Icon, &r.ForAdults)
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
		SELECT COALESCE(SUM(s.stars), 0) - COALESCE((SELECT SUM(COALESCE(rd.cost, rw.cost)) FROM redemptions rd JOIN rewards rw ON rd.reward_id = rw.id WHERE rd.user_id = ?), 0)
		FROM stars s WHERE s.user_id = ?`, userID, userID).Scan(&current)
	return current, err
}

func redeemReward(userID, rewardID int) error {
	_, err := db.Exec("INSERT INTO redemptions (user_id, reward_id, cost) SELECT ?, ?, cost FROM rewards WHERE id = ?", userID, rewardID, rewardID)
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

func valueAsString(v interface{}) (string, bool) {
	switch value := v.(type) {
	case string:
		return strings.TrimSpace(value), true
	default:
		return "", false
	}
}

func valueAsInt(v interface{}) (int, bool) {
	switch value := v.(type) {
	case int:
		return value, true
	case int64:
		return int(value), true
	case float64:
		return int(value), true
	case float32:
		return int(value), true
	default:
		return 0, false
	}
}

func valueAsBool(v interface{}) (bool, bool) {
	switch value := v.(type) {
	case bool:
		return value, true
	case string:
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "1", "true", "yes", "on":
			return true, true
		case "0", "false", "no", "off":
			return false, true
		default:
			return false, false
		}
	default:
		return false, false
	}
}

func valueAsMap(v interface{}) (map[string]interface{}, bool) {
	value, ok := v.(map[string]interface{})
	return value, ok
}

func valueAsSlice(v interface{}) ([]interface{}, bool) {
	value, ok := v.([]interface{})
	return value, ok
}

func valueAsStringMap(v interface{}) map[string]string {
	result := make(map[string]string)
	entries, ok := valueAsMap(v)
	if !ok {
		return result
	}

	for lang, raw := range entries {
		if text, ok := valueAsString(raw); ok && text != "" {
			result[lang] = text
		}
	}
	return result
}

func parseImportedTime(v interface{}) (time.Time, bool) {
	switch value := v.(type) {
	case time.Time:
		if value.IsZero() {
			return time.Time{}, false
		}
		return value, true
	case string:
		if value == "" {
			return time.Time{}, false
		}
		if t, err := time.Parse(time.RFC3339, value); err == nil {
			return t, true
		}
		if t, err := time.Parse("2006-01-02 15:04:05", value); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func normalizeImportKey(rawKey, fallback string) string {
	key := strings.TrimSpace(rawKey)
	if key == "" {
		key = sanitizeKey(fallback)
	}
	if key == "" {
		key = "custom"
	}
	if strings.ContainsAny(key, " \t\r\n") {
		key = sanitizeKey(key)
	}
	return key
}

func uniqueKeyTx(tx *sql.Tx, baseKey, table, column string) (string, error) {
	key := baseKey
	for i := 2; ; i++ {
		var count int
		if err := tx.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE "+column+" = ?", key).Scan(&count); err != nil {
			return "", err
		}
		if count == 0 {
			return key, nil
		}
		key = fmt.Sprintf("%s_%d", baseKey, i)
	}
}

func lookupUserIDTx(tx *sql.Tx, cache map[string]int, username string) (int, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return 0, errors.New("empty username")
	}
	if id, ok := cache[username]; ok {
		return id, nil
	}

	var userID int
	err := tx.QueryRow("SELECT id FROM users WHERE username = ?", username).Scan(&userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("user %q not found", username)
		}
		return 0, err
	}
	cache[username] = userID
	return userID, nil
}

func exportAllData() (map[string]interface{}, error) {
	data := make(map[string]interface{})

	users, err := getAllUsers()
	if err != nil {
		return nil, fmt.Errorf("failed to export users: %w", err)
	}
	var userExport []map[string]interface{}
	for _, u := range users {
		userExport = append(userExport, map[string]interface{}{
			"username":     u.Username,
			"is_admin":     u.IsAdmin,
			"translations": u.Translations,
		})
	}
	data["users"] = userExport

	stars, err := getStars("")
	if err != nil {
		return nil, fmt.Errorf("failed to export stars: %w", err)
	}
	var starExport []map[string]interface{}
	for _, s := range stars {
		starExport = append(starExport, map[string]interface{}{
			"username":    s.Username,
			"reason_id":   s.ReasonID,
			"reason_key":  s.ReasonKey,
			"reason_text": s.ReasonText,
			"reason_en":   getReasonText(s.ReasonID, s.ReasonText, "en"),
			"stars":       s.Stars,
			"awarded_by":  s.AwardedByName,
			"created_at":  s.CreatedAt,
		})
	}
	data["stars"] = starExport

	reasons, err := getReasons()
	if err != nil {
		return nil, fmt.Errorf("failed to export reasons: %w", err)
	}
	var reasonExport []map[string]interface{}
	for _, r := range reasons {
		reasonExport = append(reasonExport, map[string]interface{}{
			"id":           r.ID,
			"key":          r.Key,
			"translations": r.Translations,
			"stars":        r.Stars,
		})
	}
	data["reasons"] = reasonExport

	rewards, err := getRewardsList()
	if err != nil {
		return nil, fmt.Errorf("failed to export rewards: %w", err)
	}
	var rewardExport []map[string]interface{}
	for _, r := range rewards {
		rewardExport = append(rewardExport, map[string]interface{}{
			"id":           r.ID,
			"key":          r.Key,
			"name":         r.Name,
			"cost":         r.Cost,
			"icon":         r.Icon,
			"adult_only":   r.ForAdults,
			"translations": r.Translations,
		})
	}
	data["rewards"] = rewardExport

	redemptions, err := getRecentRedemptions(10000, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to export redemptions: %w", err)
	}
	var redemptionExport []map[string]interface{}
	for _, r := range redemptions {
		redemptionExport = append(redemptionExport, map[string]interface{}{
			"username":    r.Username,
			"reward_id":   r.RewardID,
			"reward_key":  r.RewardKey,
			"reward_name": r.RewardName,
			"cost":        r.Cost,
			"created_at":  r.CreatedAt,
		})
	}
	data["redemptions"] = redemptionExport

	settings := map[string]string{
		"ha_enabled":      getSetting("ha_enabled"),
		"ha_url":          getSetting("ha_url"),
		"ha_token":        getSetting("ha_token"),
		"ha_media_player": getSetting("ha_media_player"),
		"ha_lang":         getSetting("ha_lang"),
	}
	data["settings"] = settings

	return data, nil
}

func importAllData(data map[string]interface{}) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to start import transaction: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	queries := []string{
		"DELETE FROM redemptions",
		"DELETE FROM stars",
		"DELETE FROM reason_translations",
		"DELETE FROM reasons",
		"DELETE FROM reward_translations",
		"DELETE FROM rewards",
	}
	for _, query := range queries {
		if _, err := tx.Exec(query); err != nil {
			return fmt.Errorf("failed to clear existing data: %w", err)
		}
	}

	userIDCache := map[string]int{}
	reasonIDByKey := map[string]int{}
	reasonIDByEN := map[string]int{}
	reasonIDByLegacyID := map[int]int{}
	rewardIDByKey := map[string]int{}
	rewardIDByEN := map[string]int{}
	rewardIDByLegacyID := map[int]int{}

	insertReason := func(rawKey string, stars int, translations map[string]string, legacyID int) (int, error) {
		enText := strings.TrimSpace(translations["en"])
		keyBase := normalizeImportKey(rawKey, enText)
		key, err := uniqueKeyTx(tx, keyBase, "reasons", "key")
		if err != nil {
			return 0, err
		}

		result, err := tx.Exec("INSERT INTO reasons (key, stars) VALUES (?, ?)", key, stars)
		if err != nil {
			return 0, err
		}
		id64, err := result.LastInsertId()
		if err != nil {
			return 0, err
		}
		reasonID := int(id64)

		if enText == "" {
			enText = strings.TrimSpace(rawKey)
		}
		if enText == "" {
			enText = key
		}
		translations["en"] = enText

		for lang, text := range translations {
			if strings.TrimSpace(lang) == "" || strings.TrimSpace(text) == "" {
				continue
			}
			if _, err := tx.Exec("INSERT INTO reason_translations (reason_id, lang, text) VALUES (?, ?, ?)", reasonID, lang, text); err != nil {
				return 0, err
			}
		}

		reasonIDByKey[key] = reasonID
		if rawKey != "" {
			reasonIDByKey[rawKey] = reasonID
		}
		reasonIDByEN[enText] = reasonID
		if legacyID > 0 {
			reasonIDByLegacyID[legacyID] = reasonID
		}
		return reasonID, nil
	}

	rewardsHasLegacyName := columnExists("rewards", "name")
	insertReward := func(rawKey string, cost int, icon string, adultOnly bool, translations map[string]string, legacyID int) (int, error) {
		if cost < 1 {
			cost = 1
		}
		enText := strings.TrimSpace(translations["en"])
		keyBase := normalizeImportKey(rawKey, enText)
		key, err := uniqueKeyTx(tx, keyBase, "rewards", "key")
		if err != nil {
			return 0, err
		}

		if enText == "" {
			enText = strings.TrimSpace(rawKey)
		}
		if enText == "" {
			enText = key
		}
		translations["en"] = enText

		var result sql.Result
		if rewardsHasLegacyName {
			result, err = tx.Exec("INSERT INTO rewards (name, key, cost, icon, adult_only) VALUES (?, ?, ?, ?, ?)", enText, key, cost, icon, adultOnly)
		} else {
			result, err = tx.Exec("INSERT INTO rewards (key, cost, icon, adult_only) VALUES (?, ?, ?, ?)", key, cost, icon, adultOnly)
		}
		if err != nil {
			return 0, err
		}
		id64, err := result.LastInsertId()
		if err != nil {
			return 0, err
		}
		rewardID := int(id64)

		for lang, text := range translations {
			if strings.TrimSpace(lang) == "" || strings.TrimSpace(text) == "" {
				continue
			}
			if _, err := tx.Exec("INSERT INTO reward_translations (reward_id, lang, text) VALUES (?, ?, ?)", rewardID, lang, text); err != nil {
				return 0, err
			}
		}

		rewardIDByKey[key] = rewardID
		if rawKey != "" {
			rewardIDByKey[rawKey] = rewardID
		}
		rewardIDByEN[enText] = rewardID
		if legacyID > 0 {
			rewardIDByLegacyID[legacyID] = rewardID
		}
		return rewardID, nil
	}

	if rawReasons, ok := data["reasons"]; ok {
		reasons, ok := valueAsSlice(rawReasons)
		if !ok {
			return errors.New("invalid reasons payload")
		}
		for i, item := range reasons {
			entry, ok := valueAsMap(item)
			if !ok {
				return fmt.Errorf("invalid reasons entry at index %d", i)
			}
			key, _ := valueAsString(entry["key"])
			stars, hasStars := valueAsInt(entry["stars"])
			if !hasStars {
				stars = 1
			}
			translations := valueAsStringMap(entry["translations"])
			if enText, ok := valueAsString(entry["text"]); ok && translations["en"] == "" {
				translations["en"] = enText
			}
			legacyID, _ := valueAsInt(entry["id"])
			if _, err := insertReason(key, stars, translations, legacyID); err != nil {
				return fmt.Errorf("failed to import reason at index %d: %w", i, err)
			}
		}
	}

	if rawRewards, ok := data["rewards"]; ok {
		rewards, ok := valueAsSlice(rawRewards)
		if !ok {
			return errors.New("invalid rewards payload")
		}
		for i, item := range rewards {
			entry, ok := valueAsMap(item)
			if !ok {
				return fmt.Errorf("invalid rewards entry at index %d", i)
			}
			key, _ := valueAsString(entry["key"])
			legacyName, _ := valueAsString(entry["name"])
			if key == "" {
				key = legacyName
			}

			cost, hasCost := valueAsInt(entry["cost"])
			if !hasCost {
				cost = 1
			}
			icon, _ := valueAsString(entry["icon"])
			adultOnly, _ := valueAsBool(entry["adult_only"])
			translations := valueAsStringMap(entry["translations"])
			if legacyName != "" && translations["en"] == "" {
				translations["en"] = legacyName
			}
			legacyID, _ := valueAsInt(entry["id"])
			if _, err := insertReward(key, cost, icon, adultOnly, translations, legacyID); err != nil {
				return fmt.Errorf("failed to import reward at index %d: %w", i, err)
			}
		}
	}

	if rawUsers, ok := data["users"]; ok {
		users, ok := valueAsSlice(rawUsers)
		if !ok {
			return errors.New("invalid users payload")
		}
		for i, item := range users {
			entry, ok := valueAsMap(item)
			if !ok {
				return fmt.Errorf("invalid users entry at index %d", i)
			}
			username, _ := valueAsString(entry["username"])
			if username == "" {
				continue
			}
			userID, err := lookupUserIDTx(tx, userIDCache, username)
			if err != nil {
				return fmt.Errorf("failed to import user translations at index %d: %w", i, err)
			}
			if _, err := tx.Exec("DELETE FROM user_translations WHERE user_id = ?", userID); err != nil {
				return err
			}
			for lang, text := range valueAsStringMap(entry["translations"]) {
				if _, err := tx.Exec("INSERT INTO user_translations (user_id, lang, text) VALUES (?, ?, ?)", userID, lang, text); err != nil {
					return err
				}
			}
		}
	}

	if rawStars, ok := data["stars"]; ok {
		stars, ok := valueAsSlice(rawStars)
		if !ok {
			return errors.New("invalid stars payload")
		}
		for i, item := range stars {
			entry, ok := valueAsMap(item)
			if !ok {
				return fmt.Errorf("invalid stars entry at index %d", i)
			}

			username, _ := valueAsString(entry["username"])
			userID, err := lookupUserIDTx(tx, userIDCache, username)
			if err != nil {
				return fmt.Errorf("failed to import star at index %d: %w", i, err)
			}

			starsValue, hasStars := valueAsInt(entry["stars"])
			if !hasStars {
				starsValue = 1
			}

			reasonText, _ := valueAsString(entry["reason_text"])
			if reasonText == "" {
				reasonText, _ = valueAsString(entry["reason_en"])
			}

			var reasonID *int
			if legacyReasonID, ok := valueAsInt(entry["reason_id"]); ok && legacyReasonID > 0 {
				if mapped, found := reasonIDByLegacyID[legacyReasonID]; found {
					reasonID = &mapped
				}
			}
			if reasonID == nil {
				if reasonKey, ok := valueAsString(entry["reason_key"]); ok && reasonKey != "" {
					if mapped, found := reasonIDByKey[reasonKey]; found {
						reasonID = &mapped
					}
				}
			}
			if reasonID == nil && reasonText != "" {
				if mapped, found := reasonIDByEN[reasonText]; found {
					reasonID = &mapped
				}
			}
			if reasonID == nil && reasonText != "" {
				reasonKey, _ := valueAsString(entry["reason_key"])
				mapped, err := insertReason(reasonKey, starsValue, map[string]string{"en": reasonText}, 0)
				if err != nil {
					return fmt.Errorf("failed to create reason for star at index %d: %w", i, err)
				}
				reasonID = &mapped
			}

			var awardedBy interface{}
			if awardedByName, ok := valueAsString(entry["awarded_by"]); ok && awardedByName != "" {
				awarderID, err := lookupUserIDTx(tx, userIDCache, awardedByName)
				if err != nil {
					return fmt.Errorf("failed to import star awarder at index %d: %w", i, err)
				}
				awardedBy = awarderID
			}

			var reasonTextValue interface{}
			if reasonID == nil && reasonText != "" {
				reasonTextValue = reasonText
			}

			createdAt, hasCreatedAt := parseImportedTime(entry["created_at"])
			if hasCreatedAt {
				_, err = tx.Exec("INSERT INTO stars (user_id, reason_id, reason_text, stars, awarded_by, created_at) VALUES (?, ?, ?, ?, ?, ?)",
					userID, reasonID, reasonTextValue, starsValue, awardedBy, createdAt.Format(time.RFC3339))
			} else {
				_, err = tx.Exec("INSERT INTO stars (user_id, reason_id, reason_text, stars, awarded_by) VALUES (?, ?, ?, ?, ?)",
					userID, reasonID, reasonTextValue, starsValue, awardedBy)
			}
			if err != nil {
				return fmt.Errorf("failed to insert star at index %d: %w", i, err)
			}
		}
	}

	if rawRedemptions, ok := data["redemptions"]; ok {
		redemptions, ok := valueAsSlice(rawRedemptions)
		if !ok {
			return errors.New("invalid redemptions payload")
		}
		for i, item := range redemptions {
			entry, ok := valueAsMap(item)
			if !ok {
				return fmt.Errorf("invalid redemptions entry at index %d", i)
			}

			username, _ := valueAsString(entry["username"])
			userID, err := lookupUserIDTx(tx, userIDCache, username)
			if err != nil {
				return fmt.Errorf("failed to import redemption at index %d: %w", i, err)
			}

			rewardID := 0
			if legacyRewardID, ok := valueAsInt(entry["reward_id"]); ok && legacyRewardID > 0 {
				rewardID = rewardIDByLegacyID[legacyRewardID]
			}
			if rewardID == 0 {
				if rewardKey, ok := valueAsString(entry["reward_key"]); ok && rewardKey != "" {
					rewardID = rewardIDByKey[rewardKey]
				}
			}
			if rewardID == 0 {
				if rewardName, ok := valueAsString(entry["reward_name"]); ok && rewardName != "" {
					rewardID = rewardIDByEN[rewardName]
				}
			}
			if rewardID == 0 {
				return fmt.Errorf("failed to import redemption at index %d: reward not found", i)
			}

			var cost interface{}
			if parsedCost, ok := valueAsInt(entry["cost"]); ok {
				cost = parsedCost
			}

			createdAt, hasCreatedAt := parseImportedTime(entry["created_at"])
			if hasCreatedAt {
				_, err = tx.Exec("INSERT INTO redemptions (user_id, reward_id, cost, created_at) VALUES (?, ?, ?, ?)",
					userID, rewardID, cost, createdAt.Format(time.RFC3339))
			} else {
				_, err = tx.Exec("INSERT INTO redemptions (user_id, reward_id, cost) VALUES (?, ?, ?)",
					userID, rewardID, cost)
			}
			if err != nil {
				return fmt.Errorf("failed to insert redemption at index %d: %w", i, err)
			}
		}
	}

	if rawSettings, ok := data["settings"]; ok {
		settings, ok := valueAsMap(rawSettings)
		if !ok {
			return errors.New("invalid settings payload")
		}
		for key, rawValue := range settings {
			if rawValue == nil {
				continue
			}
			value, ok := valueAsString(rawValue)
			if !ok {
				value = strings.TrimSpace(fmt.Sprintf("%v", rawValue))
			}
			if _, err := tx.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)
				ON CONFLICT(key) DO UPDATE SET value = ?`, key, value, value); err != nil {
				return err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit import: %w", err)
	}
	committed = true
	return nil
}

func getRecentRedemptions(limit int, filterUserID int) ([]Redemption, error) {
	query := `SELECT rd.id, rd.user_id, u.username, rd.reward_id, rw.key, COALESCE(rd.cost, rw.cost), rd.created_at
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
		var createdAtStr sql.NullString
		err := rows.Scan(&r.ID, &r.UserID, &r.Username, &r.RewardID, &r.RewardKey, &r.Cost, &createdAtStr)
		if err != nil {
			fmt.Printf("Error scanning redemption row: %v\n", err)
			continue
		}

		// Get all translations for this user
		r.UsernameEN = getUserText(r.UserID, "en")
		r.UsernameCN = getUserText(r.UserID, "zh-CN")
		r.UsernameTW = getUserText(r.UserID, "zh-TW")

		// Get all translations for this reward
		r.RewardNameEN = getRewardText(r.RewardID, "en")
		r.RewardNameCN = getRewardText(r.RewardID, "zh-CN")
		r.RewardNameTW = getRewardText(r.RewardID, "zh-TW")
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
