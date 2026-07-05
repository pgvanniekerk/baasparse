// Minimal, dependency-free interaction layer for the baasparse GUI (stands in
// for HTMX in the alpha): dynamic transform rows, format toggles, and live
// fetch-and-swap for the transform preview and file run.
(function () {
  "use strict";

  function qs(sel, root) { return (root || document).querySelector(sel); }
  function qsa(sel, root) { return Array.prototype.slice.call((root || document).querySelectorAll(sel)); }

  // --- add a starter transform row on the builder page ---
  document.addEventListener("DOMContentLoaded", function () {
    if (qs("#field-rows") && qs("#field-rows").children.length === 0) addFieldRow();
  });

  function addFieldRow() {
    var tpl = qs("#field-row-tpl");
    var rows = qs("#field-rows");
    if (!tpl || !rows) return;
    rows.appendChild(tpl.content.cloneNode(true));
  }

  // --- event delegation ---
  document.addEventListener("click", function (e) {
    var el = e.target.closest("[data-action]");
    if (!el) return;
    var action = el.getAttribute("data-action");

    if (action === "add-field") { e.preventDefault(); addFieldRow(); }
    else if (action === "remove-field") { e.preventDefault(); var tr = el.closest("tr"); if (tr) tr.remove(); }
    else if (action === "preview") { e.preventDefault(); runPreview(); }
    else if (action === "sample") { e.preventDefault(); loadSample(el.getAttribute("data-kind")); }
  });

  document.addEventListener("change", function (e) {
    var el = e.target.closest("[data-action]");
    if (!el) return;
    var action = el.getAttribute("data-action");

    if (action === "kind") toggleFormat(el.getAttribute("data-target"), el.value);
    else if (action === "passthrough") togglePassthrough(el.checked);
    else if (action === "kind-change") toggleFieldKind(el);
  });

  function toggleFormat(target, kind) {
    qsa('[data-seg="' + target + '"] .segbtn').forEach(function (b) {
      b.classList.toggle("on", b.querySelector("input").value === kind);
    });
    qsa('[data-fmt^="' + target + '-"]').forEach(function (p) {
      p.hidden = p.getAttribute("data-fmt") !== target + "-" + kind;
    });
  }

  function togglePassthrough(on) {
    var f = qs("#fields");
    if (!f) return;
    f.style.opacity = on ? 0.4 : 1;
    f.style.pointerEvents = on ? "none" : "auto";
  }

  function toggleFieldKind(sel) {
    var tr = sel.closest("tr");
    var constIn = qs(".const-in", tr);
    var srcIn = tr.querySelector('input[name="field_source"]');
    var isConst = sel.value === "const";
    if (constIn) constIn.hidden = !isConst;
    if (srcIn) srcIn.hidden = isConst;
  }

  // --- live preview: post the builder form (urlencoded, no files) to /preview ---
  function runPreview() {
    var form = qs("#builder");
    var out = qs("#preview-out");
    if (!form || !out) return;
    out.innerHTML = '<div class="muted center">Running…</div>';
    fetch("/preview", { method: "POST", body: new URLSearchParams(new FormData(form)) })
      .then(function (r) { return r.text(); })
      .then(function (html) { out.innerHTML = html; })
      .catch(function (err) { out.innerHTML = '<div class="result-err">' + err + "</div>"; });
  }

  // --- run form on the detail page (multipart, has a file) ---
  document.addEventListener("submit", function (e) {
    var form = e.target.closest("[data-run]");
    if (!form) return;
    e.preventDefault();
    var out = qs("#run-out");
    if (out) out.innerHTML = '<div class="muted center">Processing…</div>';
    fetch(form.getAttribute("data-run"), { method: "POST", body: new FormData(form) })
      .then(function (r) { return r.text(); })
      .then(function (html) { if (out) out.innerHTML = html; })
      .catch(function (err) { if (out) out.innerHTML = '<div class="result-err">' + err + "</div>"; });
  });

  function loadSample(kind) {
    var ta = qs("#sample");
    if (!ta) return;
    if (kind === "dsv") {
      ta.value = "msisdn,seconds,cell,dropped\n27831234567,90,JHB-012,false\n27829998888,30,CPT-104,false\n27824445555,0,DBN-233,true\n";
      setKind("input", "dsv");
    } else {
      ta.value = '{"msisdn":"27831234567","seconds":90,"cell":"JHB-012","dropped":false}\n' +
                 '{"msisdn":"27829998888","seconds":30,"cell":"CPT-104","dropped":false}\n';
      setKind("input", "json");
    }
  }

  function setKind(target, kind) {
    var radio = qs('input[name="' + target + '_kind"][value="' + kind + '"]');
    if (radio) { radio.checked = true; toggleFormat(target, kind); }
  }
})();
