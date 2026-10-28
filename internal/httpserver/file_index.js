// Filters the rows of the current directory. Names come from the upstream and
// are only compared as text; nothing is written back into the document as HTML.
(function () {
  var input = document.getElementById("filter");
  if (!input) {
    return;
  }
  var rows = Array.prototype.slice.call(document.querySelectorAll("tbody tr[data-name]"));
  var empty = document.getElementById("no-match");
  input.hidden = false;
  input.addEventListener("input", function () {
    var query = input.value.trim().toLowerCase();
    var shown = 0;
    rows.forEach(function (row) {
      var match = query === "" || row.getAttribute("data-name").toLowerCase().indexOf(query) !== -1;
      row.hidden = !match;
      if (match) {
        shown++;
      }
    });
    if (empty) {
      empty.hidden = shown !== 0 || rows.length === 0;
    }
  });
})();
