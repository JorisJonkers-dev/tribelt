(function () {
  var s = document.currentScript, h = s && s.getAttribute("data-hit");
  if (!h || !navigator.sendBeacon) return;
  var shown = Date.now(), engaged = 0, sent = 0;
  function tick() { if (document.visibilityState === "visible" && shown) { engaged += Date.now() - shown; } shown = document.visibilityState === "visible" ? Date.now() : 0; }
  function send(final) {
    tick();
    if (final && engaged === sent) return;
    sent = engaged;
    navigator.sendBeacon("/b", new Blob([JSON.stringify({ h: h, e: engaged })], { type: "text/plain" }));
  }
  document.addEventListener("visibilitychange", function () { if (document.visibilityState === "hidden") send(true); else tick(); });
  addEventListener("pagehide", function () { send(true); });
  if (document.readyState === "complete") send(false); else addEventListener("load", function () { send(false); });
})();
