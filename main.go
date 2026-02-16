package main

import (
	"embed"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
)

//go:embed templates/*
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

var templates map[string]*template.Template

func main() {
	port := flag.Int("port", 8080, "HTTP port")
	dbPath := flag.String("db", "stars.db", "SQLite database path")
	flag.Parse()

	if err := initDB(*dbPath); err != nil {
		log.Fatal("Failed to init database:", err)
	}
	defer db.Close()

	if err := seedUsers(); err != nil {
		log.Fatal("Failed to seed users:", err)
	}
	if err := seedRewards(); err != nil {
		log.Fatal("Failed to seed rewards:", err)
	}

	templates = make(map[string]*template.Template)
	for _, page := range []string{"login.html", "dashboard.html", "admin.html"} {
		templates[page] = template.Must(template.ParseFS(templateFS, "templates/layout.html", "templates/"+page))
	}

	mux := http.NewServeMux()

	// Static files
	staticSub, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	// Web routes
	mux.HandleFunc("GET /{$}", authWeb(handleDashboard))
	mux.HandleFunc("GET /login", handleLoginPage)
	mux.HandleFunc("POST /login", handleLogin)
	mux.HandleFunc("POST /logout", authWeb(handleLogout))
	mux.HandleFunc("POST /star", authWeb(handleQuickStar))
	mux.HandleFunc("POST /redeem", authWeb(handleRedeem))
	mux.HandleFunc("GET /admin", authAdmin(handleAdmin))
	mux.HandleFunc("POST /admin/star", authAdmin(handleAddStar))
	mux.HandleFunc("POST /admin/apikey", authAdmin(handleGenerateAPIKey))
	mux.HandleFunc("DELETE /admin/apikey/{id}", authAdmin(handleDeleteAPIKey))

	// API routes
	mux.HandleFunc("GET /api/stars", authAPI(handleAPIGetStars))
	mux.HandleFunc("POST /api/stars", authAPI(handleAPIAddStar))
	mux.HandleFunc("GET /api/users", authAPI(handleAPIGetUsers))
	mux.HandleFunc("GET /api/reasons", authAPI(handleAPIGetReasons))

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Star Tracker listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
