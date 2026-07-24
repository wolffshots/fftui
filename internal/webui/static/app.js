// Progressive enhancement only — every view is fully functional without JS.
(function () {
  "use strict";

  var filter = document.querySelector("form.filter input[name=q]");

  // Live row filtering: mirrors the server's case-insensitive code/type
  // match (filterCycles). The GET form remains the source of truth.
  if (filter) {
    var rows = document.querySelectorAll("table.cycles-table tbody tr");
    filter.addEventListener("input", function () {
      var q = filter.value.trim().toLowerCase();
      rows.forEach(function (tr) {
        var code = tr.cells[0].textContent.toLowerCase();
        var type = tr.cells[1].textContent.toLowerCase();
        tr.hidden = q !== "" && code.indexOf(q) < 0 && type.indexOf(q) < 0;
      });
    });
  }

  // Info tooltips: tap/click toggles .open (the touch analogue of the CSS
  // hover/focus path), any other tap or Escape closes them all.
  var tips = document.querySelectorAll(".tip");
  var closeTips = function (except) {
    tips.forEach(function (t) {
      if (t !== except) t.classList.remove("open");
    });
  };
  var hoverable = window.matchMedia("(hover: hover)").matches;
  tips.forEach(function (t) {
    t.addEventListener("click", function (e) {
      if (e.target.closest(".tipbox")) return; // reading, not toggling
      if (t.closest("a")) {
        // Tip inside a sortable-header link: on hover-capable devices the
        // click sorts (hover already shows the tip); on touch, the first
        // tap peeks at the tip and a second tap follows the link.
        if (hoverable) return;
        if (!t.classList.contains("open")) e.preventDefault();
      }
      closeTips(t);
      t.classList.toggle("open");
      if (!t.classList.contains("open")) t.blur(); // :focus would keep it shown
    });
  });
  if (tips.length) {
    document.addEventListener("click", function (e) {
      if (!e.target.closest(".tip")) closeTips(null);
    });
    document.addEventListener("keydown", function (e) {
      if (e.key === "Escape") closeTips(null);
    });
  }

  // Keybindings, mirroring the TUI: 1/2/4/5 switch views, r refreshes,
  // / focuses the filter. Skipped while typing in a form field.
  var nav = { "1": "/cycles", "2": "/analytics", "4": "/charts", "5": "/live" };
  document.addEventListener("keydown", function (e) {
    if (e.ctrlKey || e.altKey || e.metaKey) return;
    var t = e.target;
    if (t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" ||
              t.tagName === "SELECT" || t.isContentEditable)) return;
    if (nav[e.key]) {
      window.location.href = nav[e.key];
    } else if (e.key === "r") {
      var f = document.querySelector("form.refresh");
      if (f) f.submit();
    } else if (e.key === "/" && filter) {
      e.preventDefault();
      filter.focus();
    }
  });
})();
