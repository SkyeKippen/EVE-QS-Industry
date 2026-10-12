// All Blueprints: each Request Blueprint button swaps itself for a runs box
// and a Confirm button. Confirm saves the request without leaving the page
// (so filters and scroll stay put) and leaves a note in its place.
(function () {
    document.querySelectorAll("[data-request-open]").forEach(function (open) {
        const cell = open.closest("td");
        const form = cell.querySelector(".request-form");
        const runs = form.querySelector("input[name=runs]");
        const confirm = form.querySelector("button[type=submit]");
        const status = cell.querySelector(".request-status");

        open.addEventListener("click", function () {
            open.hidden = true;
            form.hidden = false;
            runs.focus();
        });

        // Escape puts the button back.
        runs.addEventListener("keydown", function (event) {
            if (event.key !== "Escape") return;
            form.hidden = true;
            open.hidden = false;
            status.textContent = "";
            open.focus();
        });

        form.addEventListener("submit", function (event) {
            event.preventDefault();
            if (!form.reportValidity()) return;
            confirm.disabled = true;
            status.textContent = "";
            fetch(form.action, { method: "POST", body: new URLSearchParams(new FormData(form)) })
                .then(function (response) {
                    if (!response.ok) {
                        return response.text().then(function (text) { throw new Error(text.trim() || response.statusText); });
                    }
                    return response.json();
                })
                .then(function (saved) {
                    form.hidden = true;
                    status.textContent = "Requested " + saved.runs.toLocaleString() + (saved.runs === 1 ? " run" : " runs");
                    status.classList.remove("is-error");
                })
                .catch(function (err) {
                    status.textContent = err.message;
                    status.classList.add("is-error");
                    confirm.disabled = false;
                });
        });
    });
})();
