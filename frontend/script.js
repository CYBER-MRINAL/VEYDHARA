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