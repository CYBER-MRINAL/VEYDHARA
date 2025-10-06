package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
	_ "github.com/mattn/go-sqlite3"
)

// Page represents a single search result
type Page struct {
	URL      string `json:"url"`
	Title    string `json:"title"`
	Snippet  string `json:"snippet"`
	Category string `json:"category"`
}

// ErrorResponse represents a JSON error message
type ErrorResponse struct {
	Error string `json:"error"`
}

var (
	baseDir   string
	dbPath    string
	catPath   string
	logPath   string
	db        *sql.DB
	debugMode bool
	logger    *log.Logger
)

func init() {
	var err error
	baseDir, err = os.Getwd()
	if err != nil {
		log.Fatalf(" WORKING DIRECTORY NOT FOUND :> %v", err)
	}

	dbPath = filepath.Join(baseDir, "../database/search.db")
	catPath = filepath.Join(baseDir, "../categories.json")
	logPath = filepath.Join(baseDir, "../logs/server.log")

	os.MkdirAll(filepath.Dir(logPath), 0755)

	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}

	logger = log.New(logFile, "", log.Ldate|log.Ltime|log.Lshortfile)
	debugMode = os.Getenv("DEBUG") == "true"

	db, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		logger.Fatalf(" FAILED TO OPEN DATABASE :> %v", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)

	printBanner()
	showAvailableCategories()
	color.New(color.FgHiGreen).Println(" [^_^]> INITALIZATION SUCCESSFUL BOSS")
}

// --- /categories endpoint ---
func getCategories(w http.ResponseWriter, r *http.Request) {
	logEvent("Request", "/categories")

	data, err := os.ReadFile(catPath)
	if err != nil {
		logError("Failed to read categories.json", err)
		respondJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	var categories map[string]interface{}
	if err := json.Unmarshal(data, &categories); err != nil {
		logError("Failed to parse categories.json", err)
		respondJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	keys := make([]string, 0, len(categories))
	for k := range categories {
		keys = append(keys, k)
	}

	respondJSON(w, http.StatusOK, keys)
}

// --- /search endpoint ---
func search(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	category := strings.TrimSpace(r.URL.Query().Get("category"))

	if query == "" {
		respondJSON(w, http.StatusOK, []Page{})
		return
	}

	if category == "" {
		category = "All"
	}

	logEvent("Search", fmt.Sprintf("query='%s' category='%s'", query, category))

	var rows *sql.Rows
	var err error

	if strings.ToLower(category) != "all" {
		rows, err = db.Query(`
			SELECT url, title, snippet, category 
			FROM pages 
			WHERE LOWER(category) = LOWER(?) AND (title LIKE ? OR snippet LIKE ?) 
			LIMIT 20
		`, category, "%"+query+"%", "%"+query+"%")
	} else {
		rows, err = db.Query(`
			SELECT url, title, snippet, category 
			FROM pages 
			WHERE title LIKE ? OR snippet LIKE ? 
			LIMIT 20
		`, "%"+query+"%", "%"+query+"%")
	}

	if err != nil {
		logError("Search query failed", err)
		respondJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	defer rows.Close()

	var results []Page
	for rows.Next() {
		var p Page
		if err := rows.Scan(&p.URL, &p.Title, &p.Snippet, &p.Category); err != nil {
			continue
		}
		results = append(results, p)
	}

	if len(results) == 0 {
		logWarn(fmt.Sprintf("No results for query='%s' category='%s'", query, category))
	}

	respondJSON(w, http.StatusOK, results)
}

// --- /page endpoint ---
func getPageContent(w http.ResponseWriter, r *http.Request) {
	url := strings.TrimSpace(r.URL.Query().Get("url"))
	if url == "" {
		respondJSON(w, http.StatusBadRequest, ErrorResponse{Error: "URL parameter is required"})
		return
	}

	logEvent("PageView", url)

	row := db.QueryRow("SELECT content FROM pages WHERE url = ?", url)
	var content string

	if err := row.Scan(&content); err != nil {
		if err == sql.ErrNoRows {
			logWarn(fmt.Sprintf("No content found for URL: %s", url))
			respondJSON(w, http.StatusNotFound, ErrorResponse{Error: "Page content not found in database"})
		} else {
			logError("Failed to get page content", err)
			respondJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		}
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"content": content})
}

// --- Serve frontend ---
func serveFrontend(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	rootDir := filepath.Join(baseDir, "../frontend")

	if path == "/" || path == "" {
		http.ServeFile(w, r, filepath.Join(rootDir, "index.html"))
		return
	}

	filePath := filepath.Join(rootDir, path)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		http.ServeFile(w, r, filepath.Join(rootDir, "index.html"))
		return
	}

	http.ServeFile(w, r, filePath)
}

// --- Utility: Write JSON ---
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// --- Utility: Logging helpers ---
func logEvent(event, detail string) {
	msg := fmt.Sprintf("[%s] %s", event, detail)
	logger.Println(msg)
	color.New(color.FgHiCyan).Printf("🟢 %s\n", msg)
}

func logWarn(detail string) {
	logger.Println("WARN:", detail)
	color.New(color.FgHiYellow).Printf("🟡 WARN: %s\n", detail)
}

func logError(context string, err error) {
	msg := fmt.Sprintf("ERROR: %s -> %v", context, err)
	logger.Println(msg)
	color.New(color.FgHiRed).Printf("🔴 %s\n", msg)
}

func printBanner() {
	color.New(color.FgHiMagenta, color.Bold).Println("\n───────────────────────────────────────────────")
	color.New(color.FgHiWhite, color.Bold).Println("    🚀 VEDHARA BACKEND SERVER ")
	color.New(color.FgHiMagenta, color.Bold).Println("───────────────────────────────────────────────")
}

func showAvailableCategories() {
	data, err := os.ReadFile(catPath)
	if err != nil {
		logWarn(" FAILED TO READ CATAGORIES :-( ")
		return
	}

	var categories map[string]interface{}
	if err := json.Unmarshal(data, &categories); err != nil {
		logWarn(" FAILED TO PHASE CATAGORIES :-( ")
		return
	}

	color.New(color.FgHiGreen).Println(" 📂 AVALIABLE CATAGORIES :> ")
	for k := range categories {
		color.New(color.FgWhite).Printf("   - %s\n", k)
	}
}

// --- Main ---
func main() {
	defer db.Close()

	http.HandleFunc("/categories", getCategories)
	http.HandleFunc("/search", search)
	http.HandleFunc("/page", getPageContent)
	http.HandleFunc("/", serveFrontend)

	addr := "0.0.0.0:5000"
	color.New(color.FgHiBlue, color.Bold).Printf("\n 🌐 SERVER IS ONLINE AT :> http://%s\n", addr)
	color.New(color.FgHiCyan).Printf(" 🧠 DEBUG-MODE :> %v\n", debugMode)
	color.New(color.FgHiWhite).Println(">> LOGS ARE STORED HERE :> ", logPath)
	color.New(color.FgHiMagenta).Println("───────────────────────────────────────────────")

	if err := http.ListenAndServe(addr, nil); err != nil {
		logError(">> Server failed B-( ", err)
	}
}

