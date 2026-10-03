// Links the Quantity, Price Per Item and Price Total fields on the Create
// Order and Manage Order forms. When one price is entered and the other is
// empty, the empty one is filled in from the quantity, once: a field this
// script has filled is never overwritten, and neither is anything typed.
// The hidden price-basis field tells the server which price was typed last,
// and the note under the fields shows what will be saved.
(function () {
    const form = document.querySelector("form[data-price-fields]");
    if (!form) return;
    const quantity = form.querySelector("#order-quantity");
    const unit = form.querySelector("#order-price-unit");
    const total = form.querySelector("#order-price-total");
    const basis = form.querySelector("#price-basis");
    const note = form.querySelector("#price-note");
    const filled = { unit: false, total: false };
    const suffixes = { k: 1e3, m: 1e6, b: 1e9 };

    // Same rules as parseShorthand on the server, as whole cents. null if invalid.
    function cents(input) {
        let s = input.replace(/,/g, "").trim();
        if (s === "") return null;
        let multiplier = 1;
        const last = s.slice(-1).toLowerCase();
        if (suffixes[last]) {
            multiplier = suffixes[last];
            s = s.slice(0, -1).trim();
        }
        if (!/^(\d+\.?\d*|\.\d+)$/.test(s)) return null;
        const value = Math.round(parseFloat(s) * multiplier * 100);
        return Number.isSafeInteger(value) ? value : null;
    }

    function format(c) {
        const [whole, part] = (c / 100).toFixed(2).split(".");
        const s = whole.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
        return part === "00" ? s : s + "." + part;
    }

    // Quantity takes the same shorthand as prices ("1.2k", "1,200") but must
    // come out a whole number above zero, like parseQuantity on the server.
    function qty() {
        const c = cents(quantity.value);
        return c !== null && c > 0 && c % 100 === 0 ? c / 100 : null;
    }

    function autofill() {
        const n = qty();
        if (n === null) return;
        const u = cents(unit.value);
        const t = cents(total.value);
        if (u !== null && total.value.trim() === "" && !filled.total) {
            total.value = format(u * n);
            filled.total = true;
        } else if (t !== null && unit.value.trim() === "" && !filled.unit) {
            unit.value = format(Math.round(t / n));
            filled.unit = true;
        }
    }

    function updateNote() {
        if (!note) return;
        const n = qty();
        const u = cents(unit.value);
        const t = cents(total.value);
        let saved = null;
        if (basis.value === "unit" && u !== null && n !== null) saved = u * n;
        else if (t !== null) saved = t;
        else if (u !== null && n !== null) saved = u * n;
        note.textContent = saved === null || n === null ? "" :
            "Saves as " + format(saved) + " ISK total (" + format(Math.round(saved / n)) + " ISK per item).";
    }

    unit.addEventListener("input", function () { basis.value = "unit"; updateNote(); });
    total.addEventListener("input", function () { basis.value = "total"; updateNote(); });
    quantity.addEventListener("input", updateNote);

    // Autofill when a field is left, not on every keystroke, so typing "3.4"
    // doesn't fill the other price from the "3".
    [quantity, unit, total].forEach(function (field) {
        field.addEventListener("change", function () { autofill(); updateNote(); });
    });

    form.addEventListener("submit", function (e) {
        if (unit.value.trim() === "" && total.value.trim() === "") {
            e.preventDefault();
            unit.setCustomValidity("Enter a price per item or a price total.");
            unit.reportValidity();
        }
    });
    unit.addEventListener("input", function () { unit.setCustomValidity(""); });
    total.addEventListener("input", function () { unit.setCustomValidity(""); });

    updateNote();
})();
