// Exports a server-rendered SVG chart as PNG via canvas; no data leaves the page.
document.addEventListener("click", function (ev) {
  var btn = ev.target.closest && ev.target.closest("button.png");
  if (!btn) return;
  var svg = btn.closest("figure").querySelector("svg");
  var w = svg.viewBox.baseVal.width, h = svg.viewBox.baseVal.height, scale = 2;
  var img = new Image();
  img.onload = function () {
    var c = document.createElement("canvas");
    c.width = w * scale; c.height = h * scale;
    var ctx = c.getContext("2d");
    ctx.scale(scale, scale);
    ctx.drawImage(img, 0, 0, w, h);
    c.toBlob(function (blob) {
      var a = document.createElement("a");
      a.href = URL.createObjectURL(blob);
      a.download = (btn.getAttribute("data-name") || "chart") + ".png";
      document.body.appendChild(a); a.click(); a.remove();
      setTimeout(function () { URL.revokeObjectURL(a.href); }, 1000);
    }, "image/png");
  };
  img.src = "data:image/svg+xml;charset=utf-8," + encodeURIComponent(new XMLSerializer().serializeToString(svg));
});
