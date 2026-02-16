package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"golang.org/x/crypto/bcrypt"
)

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func handleDashboard(w http.ResponseWriter, r *http.Request) {
	user := getContextUser(r)
	counts, _ := getUserStarCounts()
	rewards, _ := getRewardsList()
	reasons, _ := getReasons()

	// Kids only see their own data; admins see everything
	var stars []Star
	var redemptions []Redemption
	if user.IsAdmin {
		stars, _ = getStars("")
		redemptions, _ = getRecentRedemptions(10, 0)
	} else {
		stars, _ = getStars(user.Username)
		redemptions, _ = getRecentRedemptions(10, user.ID)
	}

	// Consolidate sequential identical awards (same user + reason) into "N x reason"
	type DisplayStar struct {
		Star
		Display string
	}
	var consolidated []DisplayStar
	for i := 0; i < len(stars); {
		j := i + 1
		for j < len(stars) && stars[j].Username == stars[i].Username && stars[j].Reason == stars[i].Reason {
			j++
		}
		count := j - i
		ds := DisplayStar{Star: stars[i]}
		if count > 1 {
			ds.Display = fmt.Sprintf("%d × %s", count, stars[i].Reason)
		} else {
			ds.Display = stars[i].Reason
		}
		consolidated = append(consolidated, ds)
		i = j
	}

	data := map[string]interface{}{
		"User":        user,
		"StarCounts":  counts,
		"Stars":       consolidated,
		"Rewards":     rewards,
		"Redemptions": redemptions,
		"Reasons":     reasons,
	}
	templates["dashboard.html"].ExecuteTemplate(w, "dashboard.html", data)
}

func handleLoginPage(w http.ResponseWriter, r *http.Request) {
	templates["login.html"].ExecuteTemplate(w, "login.html", nil)
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	password := r.FormValue("password")

	user, err := getUserByUsername(username)
	if err != nil {
		templates["login.html"].ExecuteTemplate(w, "login.html", map[string]string{"Error": "Invalid credentials"})
		return
	}

	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		templates["login.html"].ExecuteTemplate(w, "login.html", map[string]string{"Error": "Invalid credentials"})
		return
	}

	token := randomHex(32)
	createSession(token, user.ID)

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session")
	if err == nil {
		deleteSession(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:   "session",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func handleQuickStar(w http.ResponseWriter, r *http.Request) {
	user := getContextUser(r)
	username := r.FormValue("username")
	reason := r.FormValue("reason")

	if username == "" || reason == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	if username == user.Username {
		http.Error(w, "cannot award stars to yourself", http.StatusBadRequest)
		return
	}

	addStar(username, reason, user.ID)

	if r.Header.Get("Accept") == "application/json" {
		counts, _ := getUserStarCounts()
		jsonResponse(w, map[string]interface{}{
			"counts":    counts,
			"awardedBy": user.Username,
		})
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func handleRedeem(w http.ResponseWriter, r *http.Request) {
	rewardIDStr := r.FormValue("reward_id")
	username := r.FormValue("username")

	rewardID, err := strconv.Atoi(rewardIDStr)
	if err != nil || username == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	user, err := getUserByUsername(username)
	if err != nil {
		http.Error(w, "user not found", http.StatusBadRequest)
		return
	}

	reward, err := getRewardByID(rewardID)
	if err != nil {
		http.Error(w, "reward not found", http.StatusBadRequest)
		return
	}

	current, err := getUserCurrentStars(user.ID)
	if err != nil || current < reward.Cost {
		http.Error(w, fmt.Sprintf("%s doesn't have enough stars (has %d, needs %d)", username, current, reward.Cost), http.StatusBadRequest)
		return
	}

	redeemReward(user.ID, reward.ID)

	if r.Header.Get("Accept") == "application/json" {
		counts, _ := getUserStarCounts()
		jsonResponse(w, map[string]interface{}{
			"counts":     counts,
			"rewardName": reward.Name,
			"cost":       reward.Cost,
		})
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func handleDeleteStar(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	deleteStar(id)
	counts, _ := getUserStarCounts()
	jsonResponse(w, counts)
}

func handlePasswordPage(w http.ResponseWriter, r *http.Request) {
	user := getContextUser(r)
	templates["password.html"].ExecuteTemplate(w, "password.html", map[string]interface{}{"User": user})
}

func handlePasswordChange(w http.ResponseWriter, r *http.Request) {
	user := getContextUser(r)
	current := r.FormValue("current")
	newPw := r.FormValue("new")
	confirm := r.FormValue("confirm")

	data := map[string]interface{}{"User": user}

	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(current)) != nil {
		data["Error"] = "Current password is incorrect"
		templates["password.html"].ExecuteTemplate(w, "password.html", data)
		return
	}

	if len(newPw) < 6 {
		data["Error"] = "New password must be at least 6 characters"
		templates["password.html"].ExecuteTemplate(w, "password.html", data)
		return
	}

	if newPw != confirm {
		data["Error"] = "New passwords do not match"
		templates["password.html"].ExecuteTemplate(w, "password.html", data)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPw), bcrypt.DefaultCost)
	if err != nil {
		data["Error"] = "Failed to update password"
		templates["password.html"].ExecuteTemplate(w, "password.html", data)
		return
	}

	updatePassword(user.ID, string(hash))
	data["Success"] = "Password updated successfully"
	templates["password.html"].ExecuteTemplate(w, "password.html", data)
}

func handleAdmin(w http.ResponseWriter, r *http.Request) {
	user := getContextUser(r)
	users, _ := getAllUsers()
	reasons, _ := getReasons()
	apiKeys, _ := getAPIKeys()
	rewards, _ := getRewardsList()

	data := map[string]interface{}{
		"User":    user,
		"Users":   users,
		"Reasons": reasons,
		"APIKeys": apiKeys,
		"Rewards": rewards,
	}
	templates["admin.html"].ExecuteTemplate(w, "admin.html", data)
}

func handleAddReward(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	icon := r.FormValue("icon")
	cost, err := strconv.Atoi(r.FormValue("cost"))
	if err != nil || name == "" || cost <= 0 {
		http.Error(w, "invalid reward", http.StatusBadRequest)
		return
	}
	addReward(name, cost, icon)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func handleUpdateReward(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	name := r.FormValue("name")
	icon := r.FormValue("icon")
	cost, err := strconv.Atoi(r.FormValue("cost"))
	if err != nil || name == "" || cost <= 0 {
		http.Error(w, "invalid reward", http.StatusBadRequest)
		return
	}
	updateReward(id, name, cost, icon)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func handleDeleteReward(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	deleteRewardByID(id)
	w.WriteHeader(http.StatusOK)
}

func handleAddStar(w http.ResponseWriter, r *http.Request) {
	user := getContextUser(r)
	username := r.FormValue("username")
	reason := r.FormValue("reason")

	if username == "" || reason == "" {
		http.Error(w, "username and reason required", http.StatusBadRequest)
		return
	}

	if err := addStar(username, reason, user.ID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func handleGenerateAPIKey(w http.ResponseWriter, r *http.Request) {
	label := r.FormValue("label")
	key := randomHex(32)
	keyHash := hashAPIKey(key)

	if err := addAPIKey(keyHash, label); err != nil {
		http.Error(w, "failed to create API key", http.StatusInternalServerError)
		return
	}

	// Show the key once
	user := getContextUser(r)
	users, _ := getAllUsers()
	reasons, _ := getReasons()
	apiKeys, _ := getAPIKeys()

	data := map[string]interface{}{
		"User":    user,
		"Users":   users,
		"Reasons": reasons,
		"APIKeys": apiKeys,
		"NewKey":  key,
	}
	templates["admin.html"].ExecuteTemplate(w, "admin.html", data)
}

func handleDeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	deleteAPIKey(id)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// API handlers

func handleAPIGetStars(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("user")
	stars, err := getStars(username)
	if err != nil {
		jsonError(w, "failed to get stars", http.StatusInternalServerError)
		return
	}
	if stars == nil {
		stars = []Star{}
	}
	jsonResponse(w, stars)
}

func handleAPIAddStar(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Username == "" || req.Reason == "" {
		jsonError(w, "username and reason required", http.StatusBadRequest)
		return
	}

	if err := addStar(req.Username, req.Reason, 0); err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	jsonResponse(w, map[string]string{"status": "ok"})
}

func handleAPIGetUsers(w http.ResponseWriter, r *http.Request) {
	counts, err := getUserStarCounts()
	if err != nil {
		jsonError(w, "failed to get users", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, counts)
}

func handleAPIGetReasons(w http.ResponseWriter, r *http.Request) {
	reasons, err := getReasons()
	if err != nil {
		jsonError(w, "failed to get reasons", http.StatusInternalServerError)
		return
	}
	if reasons == nil {
		reasons = []Reason{}
	}
	jsonResponse(w, reasons)
}

func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"error":%q}`, msg)
}
