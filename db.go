package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"

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

	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA foreign_keys=ON")

	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY,
		username TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		is_admin BOOLEAN DEFAULT FALSE
	);
	CREATE TABLE IF NOT EXISTS stars (
		id INTEGER PRIMARY KEY,
		user_id INTEGER NOT NULL REFERENCES users(id),
		reason TEXT NOT NULL,
		awarded_by INTEGER REFERENCES users(id),
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS reasons (
		id INTEGER PRIMARY KEY,
		text TEXT UNIQUE NOT NULL,
		count INTEGER DEFAULT 0
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
		name TEXT UNIQUE NOT NULL,
		cost INTEGER NOT NULL,
		icon TEXT NOT NULL DEFAULT ''
	);
	CREATE TABLE IF NOT EXISTS redemptions (
		id INTEGER PRIMARY KEY,
		user_id INTEGER NOT NULL REFERENCES users(id),
		reward_id INTEGER NOT NULL REFERENCES rewards(id),
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	_, err = db.Exec(schema)
	return err
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
		name string
		cost int
		icon string
	}{
		{"Extra screen time", 5, "📱"},
		{"Choose dinner", 6, "🍽️"},
		{"Stay up late", 7, "🌙"},
		{"Ice cream outing", 8, "🍦"},
		{"Movie time", 10, "🎬"},
		{"Day trip choice", 15, "🚗"},
	}

	for _, r := range rewards {
		_, err := db.Exec("INSERT INTO rewards (name, cost, icon) VALUES (?, ?, ?)", r.name, r.cost, r.icon)
		if err != nil {
			return err
		}
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
		rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsAdmin)
		users = append(users, u)
	}
	return users, nil
}

type UserStarCount struct {
	Username     string
	StarCount    int
	CurrentStars int
	IsAdmin      bool
}

func getUserStarCounts() ([]UserStarCount, error) {
	rows, err := db.Query(`
		SELECT u.username, u.is_admin, COUNT(s.id) as star_count,
			COUNT(s.id) - COALESCE((SELECT SUM(rw.cost) FROM redemptions rd JOIN rewards rw ON rd.reward_id = rw.id WHERE rd.user_id = u.id), 0) as current_stars
		FROM users u LEFT JOIN stars s ON u.id = s.user_id
		GROUP BY u.id ORDER BY star_count DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []UserStarCount
	for rows.Next() {
		var r UserStarCount
		rows.Scan(&r.Username, &r.IsAdmin, &r.StarCount, &r.CurrentStars)
		results = append(results, r)
	}
	return results, nil
}

func getStars(filterUsername string) ([]Star, error) {
	query := `SELECT s.id, s.user_id, u.username, s.reason, s.awarded_by, COALESCE(a.username,''), s.created_at
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
		rows.Scan(&s.ID, &s.UserID, &s.Username, &s.Reason, &s.AwardedBy, &s.AwardedByName, &s.CreatedAt)
		stars = append(stars, s)
	}
	return stars, nil
}

func addStar(username, reason string, awardedBy int) error {
	user, err := getUserByUsername(username)
	if err != nil {
		return fmt.Errorf("user not found: %s", username)
	}

	_, err = db.Exec("INSERT INTO stars (user_id, reason, awarded_by) VALUES (?, ?, ?)",
		user.ID, reason, awardedBy)
	if err != nil {
		return err
	}

	// Update reason frequency
	_, err = db.Exec(`INSERT INTO reasons (text, count) VALUES (?, 1)
		ON CONFLICT(text) DO UPDATE SET count = count + 1`, reason)
	return err
}

func deleteStar(id int) error {
	_, err := db.Exec("DELETE FROM stars WHERE id = ?", id)
	return err
}

func getReasons() ([]Reason, error) {
	rows, err := db.Query("SELECT id, text, count FROM reasons ORDER BY count DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reasons []Reason
	for rows.Next() {
		var r Reason
		rows.Scan(&r.ID, &r.Text, &r.Count)
		reasons = append(reasons, r)
	}
	return reasons, nil
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
	rows, err := db.Query("SELECT id, name, cost, icon FROM rewards ORDER BY cost ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rewards []Reward
	for rows.Next() {
		var r Reward
		rows.Scan(&r.ID, &r.Name, &r.Cost, &r.Icon)
		rewards = append(rewards, r)
	}
	return rewards, nil
}

func addReward(name string, cost int, icon string) error {
	_, err := db.Exec("INSERT INTO rewards (name, cost, icon) VALUES (?, ?, ?)", name, cost, icon)
	return err
}

func updateReward(id int, name string, cost int, icon string) error {
	_, err := db.Exec("UPDATE rewards SET name = ?, cost = ?, icon = ? WHERE id = ?", name, cost, icon, id)
	return err
}

func deleteRewardByID(id int) error {
	_, err := db.Exec("DELETE FROM rewards WHERE id = ?", id)
	return err
}

func getRewardByID(id int) (*Reward, error) {
	r := &Reward{}
	err := db.QueryRow("SELECT id, name, cost, icon FROM rewards WHERE id = ?", id).
		Scan(&r.ID, &r.Name, &r.Cost, &r.Icon)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func getUserCurrentStars(userID int) (int, error) {
	var current int
	err := db.QueryRow(`
		SELECT COUNT(s.id) - COALESCE((SELECT SUM(rw.cost) FROM redemptions rd JOIN rewards rw ON rd.reward_id = rw.id WHERE rd.user_id = ?), 0)
		FROM stars s WHERE s.user_id = ?`, userID, userID).Scan(&current)
	return current, err
}

func redeemReward(userID, rewardID int) error {
	_, err := db.Exec("INSERT INTO redemptions (user_id, reward_id) VALUES (?, ?)", userID, rewardID)
	return err
}

func getRecentRedemptions(limit int, filterUserID int) ([]Redemption, error) {
	query := `SELECT rd.id, rd.user_id, u.username, rw.name, rw.cost, rd.created_at
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
		rows.Scan(&r.ID, &r.UserID, &r.Username, &r.RewardName, &r.Cost, &r.CreatedAt)
		results = append(results, r)
	}
	return results, nil
}
