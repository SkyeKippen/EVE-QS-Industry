// Adds a row of filter boxes under the header of any table marked
// data-table-filter, one per column that has a heading, so each box sits in
// its column and matches its width. As the user types, rows whose cells do
// not contain every box's text (ignoring case and thousands commas) are hidden.
(function () {
    document.querySelectorAll("table[data-table-filter]").forEach(function (table) {
        const headRow = table.tHead && table.tHead.rows[0];
        const body = table.tBodies[0];
        if (!headRow || !body) return;

        const rows = Array.from(body.rows).filter(function (row) {
            return !row.querySelector("td.empty");
        });
        if (rows.length === 0) return;

        function normalize(text) {
            return text.replace(/,/g, "").replace(/\s+/g, " ").trim().toLowerCase();
        }

        // Cell text is read once; the table does not change after load.
        const cellText = rows.map(function (row) {
            return Array.from(row.cells).map(function (cell) {
                return normalize(cell.textContent);
            });
        });

        const filterRow = document.createElement("tr");
        filterRow.className = "filter-row";
        const inputs = [];
        Array.from(headRow.cells).forEach(function (th, col) {
            const cell = document.createElement("th");
            const label = th.textContent.trim();
            if (label) {
                const input = document.createElement("input");
                input.type = "search";
                input.placeholder = "Filter";
                input.setAttribute("aria-label", "Filter " + label);
                input.dataset.col = col;
                // ME/TE columns match the whole number, so "4" or "4%" finds
                // 4% but not 14%.
                if (th.classList.contains("col-efficiency")) input.dataset.exact = "";
                input.addEventListener("input", apply);
                cell.appendChild(input);
                inputs.push(input);
            }
            filterRow.appendChild(cell);
        });
        table.tHead.appendChild(filterRow);

        // Both header rows stay pinned while scrolling: the filter row sits
        // just under the heading row.
        function pin() {
            const top = headRow.offsetHeight + "px";
            Array.from(filterRow.cells).forEach(function (cell) { cell.style.top = top; });
        }
        pin();
        window.addEventListener("resize", pin);

        const noMatch = document.createElement("tr");
        noMatch.innerHTML = '<td class="empty">No orders match the filters.</td>';
        noMatch.firstChild.colSpan = headRow.cells.length;
        noMatch.hidden = true;
        body.appendChild(noMatch);

        function apply() {
            const terms = inputs
                .map(function (input) {
                    const exact = "exact" in input.dataset;
                    let text = normalize(input.value);
                    if (exact) text = text.replace(/%/g, "").trim();
                    return { col: +input.dataset.col, text: text, exact: exact };
                })
                .filter(function (term) { return term.text !== ""; });
            let shown = 0;
            rows.forEach(function (row, i) {
                const match = terms.every(function (term) {
                    const cell = cellText[i][term.col] || "";
                    if (term.exact) return cell.replace(/%/g, "").trim() === term.text;
                    return cell.includes(term.text);
                });
                row.hidden = !match;
                if (match) shown++;
            });
            noMatch.hidden = shown > 0;
        }
    });
})();
