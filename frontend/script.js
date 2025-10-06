document.addEventListener("DOMContentLoaded", () => {
  const queryInput = document.getElementById("query");
  const categorySelect = document.getElementById("category");
  const searchBtn = document.getElementById("searchBtn");
  const resultsDiv = document.getElementById("results");

  // Load categories from backend
  fetch("/categories")
    .then(res => res.json())
    .then(data => {
      data.forEach(cat => {
        const opt = document.createElement("option");
        opt.value = cat;
        opt.textContent = cat.charAt(0).toUpperCase() + cat.slice(1);
        categorySelect.appendChild(opt);
      });
    })
    .catch(err => console.error("Category load error:", err));

  // Search function
  function search() {
    const query = queryInput.value.trim();
    const category = categorySelect.value;

    if (!query) {
      resultsDiv.innerHTML = "<p> Null search... Enter somthing to search... </p>";
      return;
    }

    resultsDiv.innerHTML = "<p>Loading...</p>";

    fetch(`/search?query=${encodeURIComponent(query)}&category=${encodeURIComponent(category)}`)
      .then(res => res.json())
      .then(data => {
        if (!data.length) {
          resultsDiv.innerHTML = "<p>No results found.</p>";
          return;
        }

        resultsDiv.innerHTML = "";
        data.forEach(item => {
          const div = document.createElement("div");
          div.classList.add("result-item");

          div.innerHTML = `
            <a href="${item.url}" target="_blank">${item.title}</a>
            <p>${item.snippet || "No description available."}</p>
            <p><small>Category: ${item.category}</small></p>
          `;

          resultsDiv.appendChild(div);
        });
      })
      .catch(err => {
        console.error("Search error:", err);
        resultsDiv.innerHTML = "<p>Something went wrong.</p>";
      });
  }

  // Event listeners
  searchBtn.addEventListener("click", search);
  queryInput.addEventListener("keypress", e => {
    if (e.key === "Enter") search();
  });
});

document.addEventListener('contextmenu', function(event) {
  
  event.preventDefault();

});

// --- DOM Element References ---
const searchBtn = document.getElementById('searchBtn');
const queryInput = document.getElementById('query');
const categorySelect = document.getElementById('category');
const resultsContainer = document.getElementById('results');

// --- Functions ---

/**
 * Renders the search results on the page.
 * @param {Array} results - An array of result objects to display.
 */
function renderResults(results) {
  resultsContainer.innerHTML = '';
  if (!results || results.length === 0) {
    resultsContainer.innerHTML = '<p class="no-results">No results found. Try a different search.</p>';
    return;
  }

  results.forEach(item => {
    const resultElement = document.createElement('div');
    resultElement.className = 'result-item';
    
    // This creates a standard link that opens in a new tab.
    // Updated to use item.snippet instead of item.description
    resultElement.innerHTML = `
      <h3><a href="${item.url}" target="_blank" rel="noopener noreferrer">${item.title}</a></h3>
      <p>${item.snippet || "No description available."}</p>
      <span class="category-tag">Category: ${item.category}</span>
    `;
    resultsContainer.appendChild(resultElement);
  });
}

/**
 * Performs the search by fetching data from the backend API.
 */
async function performSearch() {
  const query = queryInput.value;
  const category = categorySelect.value;
  resultsContainer.innerHTML = '<p class="loading">Searching...</p>';
  try {
    // Correct API endpoint: /search (removed /api)
    const apiUrl = `/search?query=${encodeURIComponent(query)}&category=${encodeURIComponent(category)}`;
    const response = await fetch(apiUrl);
    if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
    const results = await response.json();
    renderResults(results);
  } catch (error) {
    console.error("Error fetching search results:", error);
    resultsContainer.innerHTML = '<p class="no-results">An error occurred while searching.</p>';
  }
}

/**
 * Populates the category dropdown by fetching from the backend.
 */
async function populateCategories() {
  try {
    // Correct API endpoint: /categories (removed /api)
    const response = await fetch('/categories');
    if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
    const categories = await response.json();
    categories.sort();
    categories.forEach(category => {
      const option = document.createElement('option');
      option.value = category;
      option.textContent = category;
      categorySelect.appendChild(option);
    });
  } catch (error) {
    console.error("Error fetching categories:", error);
  }
}

// --- Event Listeners ---
searchBtn.addEventListener('click', performSearch);
queryInput.addEventListener('keyup', (event) => {
  if (event.key === 'Enter') performSearch();
});

// --- Initial Page Load ---
document.addEventListener('DOMContentLoaded', populateCategories);

// --- Right-Click Prevention (from your code) ---
document.addEventListener('contextmenu', function(event) {
  event.preventDefault();
});
