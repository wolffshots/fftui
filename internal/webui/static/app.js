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
