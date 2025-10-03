from flask import Flask, request, jsonify, send_from_directory
import sqlite3
import json
import os

app = Flask(__name__)

BASE_DIR = os.path.dirname(os.path.abspath(__file__))
DB_PATH = os.path.join(BASE_DIR, "../database/search.db")
CAT_PATH = os.path.join(BASE_DIR, "../categories.json")

# --- Serve categories from categories.json ---
@app.route("/categories", methods=["GET"])
def get_categories():
    try:
        with open(CAT_PATH, "r") as f:
            categories = json.load(f)
        return jsonify(list(categories.keys()))
    except Exception as e:
        return jsonify({"error": str(e)}), 500

# --- Search endpoint ---
@app.route("/search", methods=["GET"])
def search():
    query = request.args.get("query", "").strip()
    category = request.args.get("category", "").strip()

    if not query:
        return jsonify([])

    conn = sqlite3.connect(DB_PATH)
    c = conn.cursor()

    if category:
        c.execute("""
            SELECT url, title, snippet, category
            FROM pages
            WHERE category = ? AND (title LIKE ? OR snippet LIKE ?)
            LIMIT 20
        """, (category, f"%{query}%", f"%{query}%"))
    else:
        c.execute("""
            SELECT url, title, snippet, category
            FROM pages
            WHERE title LIKE ? OR snippet LIKE ?
            LIMIT 20
        """, (f"%{query}%", f"%{query}%"))

    rows = c.fetchall()
    conn.close()

    results = [{"url": r[0], "title": r[1], "snippet": r[2], "category": r[3]} for r in rows]
    return jsonify(results)

# --- Serve frontend files ---
@app.route("/", defaults={"path": ""})
@app.route("/<path:path>")
def serve_frontend(path):
    root_dir = os.path.join(BASE_DIR, "../frontend")
    if path != "" and os.path.exists(os.path.join(root_dir, path)):
        return send_from_directory(root_dir, path)
    else:
        return send_from_directory(root_dir, "index.html")

if __name__ == "__main__":
    app.run(host="0.0.0.0", port=5000, debug=True)


