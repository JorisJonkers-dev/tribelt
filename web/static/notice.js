// Floating test-site notice. Loaded in <head> so a dismissed notice never paints; without JS it stays hidden.
(function () {
  var key = "tribelt-notice-dismissed", root = document.documentElement;
  function store(name) {
    try { var s = window[name]; s.getItem(key); return s; } catch (e) { return null; }
  }
  var local = store("localStorage"), session = store("sessionStorage");
  function dismissed() {
    try { return (local && local.getItem(key) === "1") || (session && session.getItem(key) === "1"); } catch (e) { return false; }
  }
  function remember() {
    try { if (local) { local.setItem(key, "1"); return; } } catch (e) { /* full or blocked: fall back */ }
    try { if (session) session.setItem(key, "1"); } catch (e) { /* hidden for this page only */ }
  }
  if (dismissed()) return;
  root.classList.add("notice-on");
  document.addEventListener("DOMContentLoaded", function () {
    var close = document.querySelector("[data-notice-close]");
    if (!close) return;
    close.addEventListener("click", function () {
      var hadFocus = document.activeElement === close;
      remember();
      root.classList.remove("notice-on");
      var main = document.getElementById("main");
      if (hadFocus && main) main.focus({ preventScroll: true });
    });
  });
})();
