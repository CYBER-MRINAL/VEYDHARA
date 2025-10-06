package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/fatih/color"
	"github.com/temoto/robotstxt"
	_ "github.com/mattn/go-sqlite3"
)

// ----------------------
// Configuration
// ----------------------
var (
	// tweak these as needed
	MaxPagesPerDomain = 50             // maximum pages to crawl per domain
	RequestTimeout    = 8 * time.Second
	PolitenessDelay   = 800 * time.Millisecond // delay between requests to same domain
	MaxWorkersPerDomain = 4                  // concurrent fetchers per domain
	MaxGlobalWorkers    = 16                 // global concurrency cap across domains
	MaxRetries          = 2                  // retry on transient HTTP errors
	UserAgent           = "CategorySearchBot/1.0"
)

// ----------------------
// Globals & Paths
// ----------------------
var (
	baseDir    string
	catPath    string
	dbDir      string
	dbPath     string
	logDir     string
	logPath    string
	debugMode  bool
	logger     *log.Logger
	db         *sql.DB
	httpClient *http.Client
)

// Page represents a row in DB
type Page struct {
	URL      string
	Title    string
	Snippet  string
	Category string
}

// ----------------------
// Main
// ----------------------
func main() {
	setupPathsAndLogging()
	defer db.Close()

	// graceful shutdown context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go monitorSignals(cancel)

	// load categories
	categories, err := loadCategories(catPath)
	if err != nil {
		logFatal("Failed to load categories: %v", err)
	}

	// create jobs
	type job struct {
		Category string
		Domain   string
	}
	var jobs []job
	for cat, domains := range categories {
		for _, d := range domains {
			dom := strings.TrimSpace(d)
			if dom == "" {
				continue
			}
			jobs = append(jobs, job{Category: cat, Domain: dom})
		}
	}

	// global worker semaphore
	globalSem := make(chan struct{}, MaxGlobalWorkers)
	var wg sync.WaitGroup

	info("Starting crawler — jobs to run: %d", len(jobs))
	for _, j := range jobs {
		select {
		case <-ctx.Done():
			warn("Shutdown requested — aborting job creation")
			break
		default:
		}
		wg.Add(1)
		globalSem <- struct{}{} // occupy one global slot
		go func(j job) {
			defer wg.Done()
			defer func() { <-globalSem }()
			domainCtx, domainCancel := context.WithCancel(ctx)
			defer domainCancel()

			// run domain crawl
			if err := crawlDomain(domainCtx, j.Category, j.Domain); err != nil {
				errLog("Domain crawl failed: %s -> %v", j.Domain, err)
			}
		}(j)
	}

	// wait for all jobs or shutdown
	wg.Wait()
	info("All crawling jobs complete")
}

// ----------------------
// Setup, Logging, DB
// ----------------------
func setupPathsAndLogging() {
	var err error
	baseDir, err = os.Getwd()
	if err != nil {
		log.Fatalf("failed to get working directory: %v", err)
	}

	// paths relative to project root
	catPath = filepath.Join(baseDir, "categories.json")
	dbDir = filepath.Join(baseDir, "database")
	dbPath = filepath.Join(dbDir, "search.db")
	logDir = filepath.Join(baseDir, "logs")
	logPath = filepath.Join(logDir, "crawler.log")

	// make dirs
	_ = os.MkdirAll(dbDir, 0755)
	_ = os.MkdirAll(logDir, 0755)

	// file logger + stdout
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("failed to open log file: %v", err)
	}
	mw := io.MultiWriter(os.Stdout, logFile)
	logger = log.New(mw, "", log.LstdFlags|log.Lshortfile)

	// debug toggle env
	debugMode = os.Getenv("DEBUG") == "true"

	// init DB
	db = initDB(dbPath)

	// http client
	httpClient = &http.Client{
		Timeout: RequestTimeout,
	}

	printBanner()
	info("DB: %s", dbPath)
	info("Categories: %s", catPath)
	info("Log: %s", logPath)
	info("Debug: %v", debugMode)
}

// initDB opens sqlite and creates table if needed
func initDB(path string) *sql.DB {
	db, err := sql.Open("sqlite3", path+"?_busy_timeout=5000")
	if err != nil {
		log.Fatalf("failed to open sqlite db: %v", err)
	}
	createStmt := `
	CREATE TABLE IF NOT EXISTS pages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		url TEXT,
		title TEXT,
		snippet TEXT,
		category TEXT
	);`
	if _, err := db.Exec(createStmt); err != nil {
		log.Fatalf("failed to create pages table: %v", err)
	}
	return db
}

func printBanner() {
	color := colorNew(colorMagenta, true)
	color("\n───────────────────────────────────────────────")
	color = colorNew(colorWhite, true)
	color("    🚀 CATEGORY CRAWLER (Go) — Polite & Robust")
	color = colorNew(colorMagenta, true)
	color("───────────────────────────────────────────────\n")
}

func monitorSignals(cancel context.CancelFunc) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	warn("Received signal %v — initiating graceful shutdown", s)
	cancel()
}

// ----------------------
// Categories loader
// ----------------------
func loadCategories(path string) (map[string][]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var categories map[string][]string
	if err := json.Unmarshal(b, &categories); err != nil {
		return nil, err
	}
	return categories, nil
}

// ----------------------
// Domain crawler
// ----------------------
func crawlDomain(ctx context.Context, category, domain string) error {
	info("Starting domain crawl: %s (category=%s)", domain, category)

	// prepare robots.txt rules
	allowAll := true
	robotsGroup, err := fetchRobotsForDomain(domain)
	if err == nil && robotsGroup != nil {
		allowAll = false
	} else if err != nil {
		// if robots can't be fetched, assume allowAll but log
		warn("Failed to fetch robots for %s: %v — continuing with polite defaults", domain, err)
	}

	// visited set and queue
	visited := make(map[string]struct{})
	visitedMu := sync.Mutex{}
	toCrawl := make(chan string, 1024)
	defer close(toCrawl)

	// per-domain rate limiter
	ticker := time.NewTicker(PolitenessDelay)
	defer ticker.Stop()

	// seed URL(s): try https then http fallback
	seeds := []string{"https://" + domain, "http://" + domain}
	var seedURL string
	for _, s := range seeds {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if ok := testURLReachable(s); ok {
			seedURL = s
			break
		}
	}
	if seedURL == "" {
		return fmt.Errorf("seed not reachable for domain %s", domain)
	}

	enqueue := func(u string) {
		visitedMu.Lock()
		defer visitedMu.Unlock()
		if _, seen := visited[u]; seen {
			return
		}
		visited[u] = struct{}{}
		select {
		case toCrawl <- u:
		default:
			// if channel full, drop politely
			warn("queue full, dropping URL: %s", u)
		}
	}

	enqueue(seedURL)

	// worker pool for domain
	workerWG := sync.WaitGroup{}
	sem := make(chan struct{}, MaxWorkersPerDomain)

	crawledCount := 0
	crawledMu := sync.Mutex{}

	// shutdown watcher
	stop := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(stop)
	}()

	for {
		// stop conditions
		crawledMu.Lock()
		if crawledCount >= MaxPagesPerDomain {
			crawledMu.Unlock()
			break
		}
		crawledMu.Unlock()

		select {
		case <-ctx.Done():
			info("context cancelled for domain %s", domain)
			break
		case u, ok := <-toCrawl:
			if !ok {
				break
			}

			// Respect robots if available
			if !allowAll && robotsGroup != nil {
				parsed, perr := url.Parse(u)
				if perr == nil {
					if !robotsGroup.Test(parsed.RequestURI()) {
						info("Robots disallow: %s", u)
						continue
					}
				}
			}

			// check limit again
			crawledMu.Lock()
			if crawledCount >= MaxPagesPerDomain {
				crawledMu.Unlock()
				break
			}
			crawledMu.Unlock()

			// worker semaphore & launch
			sem <- struct{}{}
			workerWG.Add(1)
			go func(pageURL string) {
				defer workerWG.Done()
				defer func() { <-sem }()

				// wait politeness ticker
				select {
				case <-ticker.C:
				case <-stop:
					return
				}

				// fetch & process with retries
				var respBody io.ReadCloser
				var finalURL string
				var err error
				for attempt := 0; attempt <= MaxRetries; attempt++ {
					finalURL, respBody, err = fetchURLWithBody(pageURL)
					if err == nil && respBody != nil {
						break
					}
					// backoff
					sleep := time.Duration((attempt+1)*(attempt+1)) * 200 * time.Millisecond
					time.Sleep(sleep)
				}
				if err != nil {
					errLog("Failed fetch %s: %v", pageURL, err)
					return
				}
				defer respBody.Close()

				// parse and extract
				doc, err := goquery.NewDocumentFromReader(respBody)
				if err != nil {
					errLog("Failed parse HTML %s: %v", pageURL, err)
					return
				}

				title := strings.TrimSpace(doc.Find("title").First().Text())
				if title == "" {
					title = "No Title"
				}
				snippet := ""
				if desc, ok := doc.Find(`meta[name="description"]`).Attr("content"); ok {
					snippet = strings.TrimSpace(desc)
				} else {
					snippet = strings.TrimSpace(doc.Find("p").First().Text())
				}

				// persist
				if err := savePage(Page{
					URL:      finalURL,
					Title:    title,
					Snippet:  snippet,
					Category: category,
				}); err != nil {
					errLog("DB save failed for %s: %v", finalURL, err)
				} else {
					info("[Saved] %s", finalURL)
				}

				// increment count
				crawledMu.Lock()
				crawledCount++
				crawledMu.Unlock()

				// discover internal links and enqueue
				doc.Find("a[href]").Each(func(i int, s *goquery.Selection) {
					href, ok := s.Attr("href")
					if !ok || href == "" {
						return
					}
					abs := toAbsoluteURL(finalURL, href)
					if abs == "" {
						return
					}
					// domain restriction (allow subdomains)
					u, perr := url.Parse(abs)
					if perr != nil {
						return
					}
					if strings.HasSuffix(u.Hostname(), domain) {
						// normalize (strip fragment)
						u.Fragment = ""
						abs = u.String()
						visitedMu.Lock()
						if _, seen := visited[abs]; !seen {
							visited[abs] = struct{}{}
							// only enqueue if limit not reached
							crawledMu.Lock()
							if crawledCount < MaxPagesPerDomain {
								select {
								case toCrawl <- abs:
								default:
									// queue full, skip
								}
							}
							crawledMu.Unlock()
						}
						visitedMu.Unlock()
					}
				})
			}(u)

		case <-time.After(500 * time.Millisecond):
			// nothing enqueued recently; if workers idle and queue empty, finish
			crawledMu.Lock()
			if crawledCount >= MaxPagesPerDomain {
				crawledMu.Unlock()
				break
			}
			crawledMu.Unlock()
			// check stop signal
			select {
			case <-stop:
				info("stop signal received for domain %s", domain)
				break
			default:
			}
		}
	}

	// wait for workers finish
	workerWG.Wait()
	info("Finished domain crawl: %s (crawled=%d)", domain, crawledCount)
	return nil
}

// ----------------------
// Helpers: HTTP, Robots, Fetching
// ----------------------
func testURLReachable(u string) bool {
	req, _ := http.NewRequest("HEAD", u, nil)
	req.Header.Set("User-Agent", UserAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

func fetchRobotsForDomain(domain string) (*robotstxt.Group, error) {
	robotsURL := "https://" + domain + "/robots.txt"
	req, _ := http.NewRequest("GET", robotsURL, nil)
	req.Header.Set("User-Agent", UserAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		// try http fallback
		robotsURL = "http://" + domain + "/robots.txt"
		req, _ = http.NewRequest("GET", robotsURL, nil)
		req.Header.Set("User-Agent", UserAgent)
		resp, err = httpClient.Do(req)
		if err != nil {
			return nil, err
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("robots returned status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	robots, err := robotstxt.FromBytes(data)
	if err != nil {
		return nil, err
	}
	group := robots.FindGroup(UserAgent)
	return group, nil
}

// fetchURLWithBody GETs URL and returns final URL (after redirects) and response body reader
func fetchURLWithBody(u string) (string, io.ReadCloser, error) {
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", UserAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", nil, err
	}
	// accept only HTML
	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		resp.Body.Close()
		return "", nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	if !strings.Contains(ct, "html") {
		// read body then close and return error — we don't index non-HTML
		resp.Body.Close()
		return "", nil, errors.New("non-html content")
	}
	// resp.Body will be closed by caller
	return resp.Request.URL.String(), resp.Body, nil
}

// ----------------------
// DB persistence
// ----------------------
func savePage(p Page) error {
	stmt := `INSERT INTO pages (url, title, snippet, category) VALUES (?, ?, ?, ?)`
	_, err := db.Exec(stmt, p.URL, p.Title, p.Snippet, p.Category)
	return err
}

// ----------------------
// Utilities
// ----------------------
func toAbsoluteURL(base, href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if u.IsAbs() {
		// normalize: strip fragment
		u.Fragment = ""
		return u.String()
	}
	baseU, err := url.Parse(base)
	if err != nil {
		return ""
	}
	resolved := baseU.ResolveReference(u)
	resolved.Fragment = ""
	return resolved.String()
}

// ----------------------
// Logging helpers (color + file)
// ----------------------
type colorFn func(format string, a ...interface{})

var (
	colorRed     = color.New(color.FgHiRed).PrintfFunc()
	colorYellow  = color.New(color.FgHiYellow).PrintfFunc()
	colorGreen   = color.New(color.FgHiGreen).PrintfFunc()
	colorCyan    = color.New(color.FgHiCyan).PrintfFunc()
	colorMagenta = color.New(color.FgHiMagenta).PrintfFunc()
	colorWhite   = color.New(color.FgHiWhite).PrintfFunc()
)

func colorNew(fn colorFn, bold bool) func(s string) {
	return func(s string) {
		if bold {
			fmt.Println(s)
			return
		}
		fmt.Println(s)
	}
}

func info(format string, a ...interface{}) {
	logger.Printf("[INFO] "+format, a...)
	if debugMode {
		colorCyan("[INFO] "+format+"\n", a...)
	} else {
		colorWhite("[INFO] "+format+"\n", a...)
	}
}

func warn(format string, a ...interface{}) {
	logger.Printf("[WARN] "+format, a...)
	colorYellow("[WARN] "+format+"\n", a...)
}

func errLog(format string, a ...interface{}) {
	logger.Printf("[ERROR] "+format, a...)
	colorRed("[ERROR] "+format+"\n", a...)
}

func logFatal(format string, a ...interface{}) {
	logger.Fatalf("[FATAL] "+format, a...)
	colorRed("[FATAL] "+format+"\n", a...)
	os.Exit(1)
}

