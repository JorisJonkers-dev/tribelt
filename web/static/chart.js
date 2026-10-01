// Progressive enhancement for /stats; every control also works as a plain link or form without it.
(function () {
  document.documentElement.classList.add("js");

  // PNG export: rasterise the chart's same-origin SVG export on a canvas; no data leaves the page.
  function png(btn) {
    var img = new Image();
    img.onload = function () {
      var scale = 2, c = document.createElement("canvas");
      c.width = img.naturalWidth * scale;
      c.height = img.naturalHeight * scale;
      var ctx = c.getContext("2d");
      ctx.scale(scale, scale);
      ctx.drawImage(img, 0, 0);
      c.toBlob(function (blob) {
        var a = document.createElement("a");
        a.href = URL.createObjectURL(blob);
        a.download = (btn.getAttribute("data-name") || "chart") + ".png";
        document.body.appendChild(a);
        a.click();
        a.remove();
        setTimeout(function () { URL.revokeObjectURL(a.href); }, 1000);
      }, "image/png");
    };
    img.src = btn.getAttribute("data-png");
  }

  // Legend chips and segmented toggles swap their section in place instead of reloading the page.
  function swap(link, ev) {
    if (ev.metaKey || ev.ctrlKey || ev.shiftKey || ev.altKey || ev.button !== 0) return;
    ev.preventDefault();
    var id = link.getAttribute("data-swap"), key = link.getAttribute("data-key"), href = link.href;
    fetch(href, { credentials: "same-origin" }).then(function (r) {
      if (!r.ok) throw new Error(r.status);
      return r.text();
    }).then(function (html) {
      var doc = new DOMParser().parseFromString(html, "text/html");
      var ids = [id];
      document.querySelectorAll("[data-sync]").forEach(function (el) { ids.push(el.id); });
      ids.forEach(function (i) {
        var cur = document.getElementById(i), next = doc.getElementById(i);
        if (cur && next) cur.replaceWith(document.importNode(next, true));
      });
      history.replaceState(null, "", href);
      var again = key && document.querySelector('#' + id + ' [data-key="' + key + '"]');
      (again || document.getElementById(id)).focus({ preventScroll: true });
    }).catch(function () { location.href = href; });
  }

  document.addEventListener("click", function (ev) {
    var t = ev.target.closest ? ev.target : null;
    if (!t) return;
    var btn = t.closest("button[data-png]");
    if (btn) { png(btn); return; }
    var link = t.closest("a[data-swap]");
    if (link) swap(link, ev);
  });

  // Filter selects apply on change; the Apply buttons are hidden once this runs.
  document.addEventListener("change", function (ev) {
    var form = ev.target.closest && ev.target.closest("form[data-autosubmit]");
    if (form && ev.target.type !== "search") form.requestSubmit();
  });

  // Live filter of the page list as you type; the form still submits ?q= without JS.
  document.addEventListener("input", function (ev) {
    var sel = ev.target.getAttribute && ev.target.getAttribute("data-filter");
    if (!sel) return;
    var q = ev.target.value.toLowerCase();
    document.querySelectorAll(sel + " [data-row]").forEach(function (row) {
      row.hidden = q !== "" && row.textContent.toLowerCase().indexOf(q) < 0;
    });
  });
})();
