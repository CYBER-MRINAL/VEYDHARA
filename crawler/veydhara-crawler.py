import os
import json
import re
import sqlite3
import requests
from bs4 import BeautifulSoup
from urllib.parse import urljoin, urlparse
from collections import deque

# --- Setup paths ---
BASE_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CATEGORIES_FILE = os.path.join(BASE_DIR, "categories.json")
DB_DIR = os.path.join(BASE_DIR, "database")
DB_PATH = os.path.join(DB_DIR, "search.db")

# --- Ensure database folder exists ---
os.makedirs(DB_DIR, exist_ok=True)

# --- Load categories ---
with open(CATEGORIES_FILE, "r") as f:
    categories = json.load(f)

# --- Setup database ---
conn = sqlite3.connect(DB_PATH)
c = conn.cursor()
c.execute("""
CREATE TABLE IF NOT EXISTS pages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    url TEXT,
    title TEXT,
    snippet TEXT,
    category TEXT
)
""")
conn.commit()

def clean_text(text: str) -> str:
    """Remove extra whitespace and special chars."""
    return re.sub(r'\s+', ' ', text).strip() if text else ""

def save_page(url: str, title: str, snippet: str, category: str):
    """Insert page into DB."""
    c.execute("INSERT INTO pages (url, title, snippet, category) VALUES (?, ?, ?, ?)",
              (url, title, snippet, category))
    conn.commit()
    print(f"[OK] Saved: {url}")

def crawl_domain(domain: str, category: str, max_pages: int = 10):
    """Crawl a domain (up to max_pages) and save results."""
    visited = set()
    queue = deque([f"https://{domain}"])

    while queue and len(visited) < max_pages:
        url = queue.popleft()
        if url in visited:
            continue
        visited.add(url)

        try:
            print(f"[CRAWL] {url}")
            r = requests.get(url, timeout=8, headers={"User-Agent": "CategorySearchBot/1.0"})
            if r.status_code != 200:
                continue

            soup = BeautifulSoup(r.text, "html.parser")

            # Extract title + snippet
            title = soup.title.string if soup.title else "No Title"
            snippet = ""
            if soup.find("meta", attrs={"name": "description"}):
                snippet = soup.find("meta", attrs={"name": "description"}).get("content")
            elif soup.find("p"):
                snippet = soup.find("p").get_text()

            title = clean_text(title)
            snippet = clean_text(snippet)
            save_page(url, title, snippet, category)

            # Find internal links (stay inside domain)
            for a in soup.find_all("a", href=True):
                link = urljoin(url, a["href"])
                if urlparse(link).netloc.endswith(domain) and link not in visited:
                    queue.append(link)

        except Exception as e:
            print(f"[ERR] {url}: {e}")

def run_crawler():
    for category, domains in categories.items():
        for domain in domains:
            crawl_domain(domain, category, max_pages=10)

if __name__ == "__main__":
    run_crawler()
    conn.close()

