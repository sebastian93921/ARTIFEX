/* ARTIFEX landing — minimal progressive enhancement.
   No dependencies, no network calls. Everything degrades to readable HTML
   when JavaScript is off. Localised feedback strings follow <html lang>. */
(function () {
  "use strict";

  var isKo = (document.documentElement.lang || "en").indexOf("ko") === 0;
  var T = isKo
    ? { copy: "복사", copied: "복사됨", copyFail: "직접 선택하세요", menu: "메뉴" }
    : { copy: "Copy", copied: "Copied", copyFail: "Select manually", menu: "Menu" };

  /* ---- Mobile navigation toggle ---- */
  var toggle = document.querySelector(".nav-toggle");
  var nav = document.getElementById("primary-nav");
  if (toggle && nav) {
    toggle.addEventListener("click", function () {
      var open = nav.classList.toggle("is-open");
      toggle.setAttribute("aria-expanded", open ? "true" : "false");
    });
    nav.addEventListener("click", function (e) {
      if (e.target.closest("a")) {
        nav.classList.remove("is-open");
        toggle.setAttribute("aria-expanded", "false");
      }
    });
  }

  /* ---- Generic ARIA tablist controller ----
     Works for the walkthrough (vertical) and agent bridge (horizontal). */
  function wireTablist(list) {
    var tabs = Array.prototype.slice.call(
      list.querySelectorAll('[role="tab"]')
    );
    if (!tabs.length) return;
    var vertical = list.getAttribute("aria-orientation") === "vertical";

    function panelFor(tab) {
      return document.getElementById(tab.getAttribute("aria-controls"));
    }

    function select(tab, focus) {
      tabs.forEach(function (t) {
        var on = t === tab;
        t.setAttribute("aria-selected", on ? "true" : "false");
        t.tabIndex = on ? 0 : -1;
        var panel = panelFor(t);
        if (panel) {
          if (on) {
            // brief swap state so the screenshot cross-fades, not jumps
            panel.classList.add("is-swapping");
            panel.hidden = false;
            requestAnimationFrame(function () {
              requestAnimationFrame(function () {
                panel.classList.remove("is-swapping");
              });
            });
          } else {
            panel.hidden = true;
          }
        }
      });
      if (focus) tab.focus();
    }

    tabs.forEach(function (tab, i) {
      tab.addEventListener("click", function () {
        select(tab, false);
      });
      tab.addEventListener("keydown", function (e) {
        var next = (vertical && e.key === "ArrowDown") ||
          (!vertical && e.key === "ArrowRight");
        var prev = (vertical && e.key === "ArrowUp") ||
          (!vertical && e.key === "ArrowLeft");
        var idx = -1;
        if (next) idx = (i + 1) % tabs.length;
        else if (prev) idx = (i - 1 + tabs.length) % tabs.length;
        else if (e.key === "Home") idx = 0;
        else if (e.key === "End") idx = tabs.length - 1;
        if (idx >= 0) {
          e.preventDefault();
          select(tabs[idx], true);
        }
      });
    });
  }
  Array.prototype.slice
    .call(document.querySelectorAll('[role="tablist"]'))
    .forEach(wireTablist);

  /* ---- Copy-to-clipboard buttons ---- */
  function flash(btn, label, copied) {
    var text = btn.querySelector(".copy-btn__text");
    if (text) text.textContent = label;
    btn.setAttribute("data-copied", copied ? "true" : "false");
    btn.setAttribute("aria-label", label);
    window.clearTimeout(btn._t);
    btn._t = window.setTimeout(function () {
      if (text) text.textContent = T.copy;
      btn.setAttribute("data-copied", "false");
      btn.setAttribute("aria-label", T.copy);
    }, 2000);
  }

  Array.prototype.slice
    .call(document.querySelectorAll(".copy-btn"))
    .forEach(function (btn) {
      var text = btn.querySelector(".copy-btn__text");
      if (text) text.textContent = T.copy;
      btn.setAttribute("aria-label", T.copy);
      btn.addEventListener("click", function () {
        var sel = btn.getAttribute("data-copy-target");
        var src = sel ? document.querySelector(sel) : null;
        var value = src ? src.innerText.replace(/\s+$/, "") : "";
        if (!value) return;
        if (navigator.clipboard && navigator.clipboard.writeText) {
          navigator.clipboard.writeText(value).then(
            function () {
              flash(btn, T.copied, true);
            },
            function () {
              flash(btn, T.copyFail, false);
            }
          );
        } else {
          flash(btn, T.copyFail, false);
        }
      });
    });
})();
