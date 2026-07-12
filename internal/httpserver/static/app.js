// Minimal, dependency-free interaction layer for the baasparse GUI (stands in
// for HTMX in the alpha): the stepped pipeline-creation wizard, dynamic declared
// input fields and transform rows with source dropdowns, backend/format toggles,
// and live fetch-and-swap for the transform preview and file run.
(function () {
  "use strict";

  function qs(sel, root) { return (root || document).querySelector(sel); }
  function qsa(sel, root) { return Array.prototype.slice.call((root || document).querySelectorAll(sel)); }

  // --- seed starter rows on the builder page ---
  document.addEventListener("DOMContentLoaded", function () {
    if (qs("#decl-rows") && qs("#decl-rows").children.length === 0) addDeclRow();
    // Seed the common single-destination case so it needs no clicks at all.
    if (qs("#dest-rows") && dests.length === 0) { dests.push(newDest("default")); renderDests(); }
  });

  // ================= wizard steps =================
  function showStep(n) {
    qsa(".step").forEach(function (sec) {
      sec.hidden = sec.getAttribute("data-step") !== String(n);
    });
    qsa("#stepper li").forEach(function (li) {
      var s = Number(li.getAttribute("data-step"));
      li.classList.toggle("on", s === n);
      li.classList.toggle("done", s < n);
    });
    window.scrollTo(0, 0);
  }
  function currentStep() {
    var vis = qsa(".step").filter(function (s) { return !s.hidden; })[0];
    return vis ? Number(vis.getAttribute("data-step")) : 1;
  }
  function stepSection(n) {
    return qsa(".step").filter(function (s) { return Number(s.getAttribute("data-step")) === n; })[0];
  }
  // Move to `target`, but never skip FORWARD past a step that lacks the minimum
  // information the later steps need — stop on the first invalid step and say why.
  function advanceStep(target) {
    var cur = currentStep();
    target = Math.max(1, Math.min(target, 4));
    if (target > cur) {
      for (var s = cur; s < target; s++) {
        var err = validateStep(s);
        if (err) { showStep(s); showStepError(stepSection(s), err); return; }
      }
    }
    clearAllErrors();
    showStep(target);
  }

  // ================= step validation =================
  function fval(name) { var el = qs('[name="' + name + '"]'); return el ? String(el.value || "").trim() : ""; }
  function declaredNames() {
    return qsa('#decl-rows input[name="decl_name"]')
      .map(function (i) { return i.value.trim(); })
      .filter(function (v) { return v !== ""; });
  }
  // validateStep returns a human message when step n lacks the minimum the later
  // steps need, or "" when it is OK to proceed.
  function validateStep(n) {
    if (n === 1) {
      if (!fval("name")) return "Give the pipeline a name.";
      var inHidden = qs('[name="datasource_id"]');
      if (!inHidden || !inHidden.value) return "Choose an input datasource.";
      if (inHidden.getAttribute("data-kind") === "s3" && !fval("ds_bucket")) return "Enter the S3 bucket for this pipeline.";
      var outHidden = qs('[name="output_datasource_id"]');
      if (outHidden && outHidden.value && outHidden.getAttribute("data-kind") === "s3" &&
          !fval("ds_output_bucket") && inHidden.getAttribute("data-kind") !== "s3")
        return "Enter the S3 bucket for the output datasource.";
      return "";
    }
    if (n === 2) {
      if (declaredNames().length === 0) return "Declare at least one input field — the output step maps its fields from these.";
      return "";
    }
    if (n === 3) {
      if (dests.length === 0) return "Add at least one destination.";
      return "";
    }
    return "";
  }
  function showStepError(sec, msg) {
    if (!sec) return;
    var el = qs(".wiz-err", sec);
    if (!el) {
      el = document.createElement("div");
      el.className = "wiz-err";
      el.setAttribute("role", "alert");
      sec.insertBefore(el, qs(".wiznav", sec));
    }
    el.textContent = msg;
    el.hidden = false;
  }
  function clearStepError(sec) { var el = sec && qs(".wiz-err", sec); if (el) el.hidden = true; }
  function clearAllErrors() { qsa(".wiz-err").forEach(function (el) { el.hidden = true; }); }

  // ================= dynamic rows =================
  function addDeclRow() {
    var tpl = qs("#decl-row-tpl"), rows = qs("#decl-rows");
    if (tpl && rows) rows.appendChild(tpl.content.cloneNode(true));
  }

  // Rebuild every output-row source dropdown from the declared input fields,
  // preserving the current selection where still valid.

  // ================= click delegation =================
  document.addEventListener("click", function (e) {
    var el = e.target.closest("[data-action]");
    if (!el) return;
    var action = el.getAttribute("data-action");

    if (action === "next-step") { e.preventDefault(); advanceStep(currentStep() + 1); }
    else if (action === "prev-step") { e.preventDefault(); clearAllErrors(); showStep(Math.max(currentStep() - 1, 1)); }
    else if (action === "goto-step") { e.preventDefault(); advanceStep(Number(el.getAttribute("data-step"))); }
    else if (action === "add-decl") { e.preventDefault(); addDeclRow(); }
    else if (action === "remove-decl") { e.preventDefault(); var dr = el.closest("tr"); if (dr) dr.remove(); }
    else if (action === "parse-fields") { e.preventDefault(); parseFieldsFromSample(); }
    else if (action === "preview") { e.preventDefault(); runPreview(); }
    else if (action === "sample") { e.preventDefault(); loadSample(el.getAttribute("data-kind")); }
    else if (action === "test-conn") { e.preventDefault(); testConnection(el); }
    else if (action === "open-ds-picker") { e.preventDefault(); openDsPicker(el.getAttribute("data-target")); }
    else if (action === "close-ds-picker") { e.preventDefault(); closeDsPicker(); }
    else if (action === "choose-ds") { e.preventDefault(); chooseDs(el); }
    else if (action === "clear-ds") { e.preventDefault(); clearDs(el.getAttribute("data-target")); }
    else if (action === "add-dest") { e.preventDefault(); openDest(); }
    else if (action === "edit-dest") { e.preventDefault(); openDest(Number(el.closest("[data-dest-row]").getAttribute("data-index"))); }
    else if (action === "del-dest") { e.preventDefault(); dests.splice(Number(el.closest("[data-dest-row]").getAttribute("data-index")), 1); renderDests(); }
    else if (action === "close-dest") { e.preventDefault(); closeDest(); }
    else if (action === "save-dest") { e.preventDefault(); saveDest(); }
    else if (action === "dm-add-field") { e.preventDefault(); addFieldRow(); }
    else if (action === "dm-add-all") { e.preventDefault(); addAllFields(); }
    else if (action === "dm-clear-fields") { e.preventDefault(); qs("#dm-fields").innerHTML = ""; syncFieldsTable(); }
    else if (action === "dm-del-field") { e.preventDefault(); el.closest("tr").remove(); syncFieldsTable(); }
    else if (action === "dm-field-up") { e.preventDefault(); moveFieldRow(el, -1); }
    else if (action === "dm-field-down") { e.preventDefault(); moveFieldRow(el, 1); }
  });

  // close the datasource picker on Escape
  document.addEventListener("keydown", function (e) {
    if (e.key !== "Escape") return;
    closeDsPicker();
    if (qs("#dest-modal") && !qs("#dest-modal").hidden) closeDest();
  });

  // ================= destinations =================
  //
  // Destinations are held as a small model array and rendered into the table as
  // rows of HIDDEN inputs. The form therefore still posts one value per field per
  // row (parallel arrays the server reads by index) — which is why every field is
  // written unconditionally, even the ones that do not apply to a row's kind: a
  // missing value would shift every later row's values up by one and silently give
  // destination 2 destination 3's format.
  var dests = [];
  var editingIndex = -1;

  function newDest(name) {
    return {
      name: name || "", kind: "file",
      datasource: "", bucket: "", dir: "",
      format: "json", delimiter: "comma", hasHeader: "1",
      jsonMode: "ndjson", xmlRoot: "", xmlRecord: "",
      // This destination's OWN output structure: an ORDERED list of fields, each
      // drawn from an input field, a constant, or a concatenation.
      passThrough: true,
      fields: [],    // {output, kind: field|const|concat, source, value, type}
      compress: "0",
      table: "", dbMode: "insert",
    };
  }

  // destJSON is the single self-describing blob each row posts. One blob per row
  // instead of fifteen parallel arrays: a row can no longer omit a value and shift
  // every later row's fields by one.
  function destJSON(d) {
    var fields = [];
    if (!d.passThrough) {
      d.fields.forEach(function (f) {
        if (!f.output) return;
        if (f.kind === "concat") {
          fields.push({
            output: f.output, kind: "concat", type: f.type, sep: "",
            parts: (f.value || "").split(",").map(function (p) { return p.trim(); }).filter(Boolean),
          });
        } else if (f.kind === "const") {
          fields.push({ output: f.output, kind: "const", const: f.value || "", type: f.type });
        } else {
          fields.push({ output: f.output, kind: "field", source: f.source || f.output, type: f.type });
        }
      });
    }
    return JSON.stringify({
      name: d.name, kind: d.kind,
      datasourceID: d.datasource ? Number(d.datasource) : 0,
      bucket: d.bucket, dir: d.dir,
      format: {
        kind: d.format, delimiter: d.delimiter, hasHeader: d.hasHeader === "1",
        jsonMode: d.jsonMode, xmlRoot: d.xmlRoot, xmlRecord: d.xmlRecord,
      },
      compress: d.compress === "1",
      passThrough: d.passThrough,
      fields: fields,
      table: d.table, dbMode: d.dbMode,
    });
  }

  // ---- destinations table ----
  function renderDests() {
    var host = qs("#dest-rows"), tpl = qs("#dest-row-tpl"), empty = qs("#dest-empty");
    if (!host || !tpl) return;
    host.innerHTML = "";
    dests.forEach(function (d, i) {
      var row = tpl.content.firstElementChild.cloneNode(true);
      row.setAttribute("data-index", i);
      qs('[data-c="name"]', row).textContent = d.name;
      qs('[data-c="kind"]', row).textContent = d.kind === "rdbms" ? "DB" : "FILE";
      qs('[data-c="where"]', row).textContent = destWhere(d);
      qs('[data-c="format"]', row).textContent = destFormatLabel(d);
      qs('[data-c="columns"]', row).textContent = fieldsLabel(d);
      qs('[data-c="delivery"]', row).textContent =
        d.kind === "rdbms" ? "—" : (d.compress === "1" ? "gzip" : "plain");

      // One self-describing blob per row — see destJSON.
      var inp = document.createElement("input");
      inp.type = "hidden"; inp.name = "dest_json"; inp.value = destJSON(d);
      qs("td", row).appendChild(inp);
      host.appendChild(row);
    });
    if (empty) empty.hidden = dests.length > 0;
  }

  function fieldsLabel(d) {
    if (d.passThrough) return "all input fields";
    var names = d.fields.filter(function (f) { return f.output; }).map(function (f) {
      return f.kind === "field" ? f.output : f.output + "*"; // * = derived
    });
    if (!names.length) return "(none)";
    if (names.length <= 3) return names.join(", ");
    return names.slice(0, 3).join(", ") + " +" + (names.length - 3);
  }

  function destWhere(d) {
    if (d.kind === "rdbms") return d.table || "(no table)";
    var ds = d.datasource ? dsName(d.datasource) : "pipeline";
    return d.dir ? ds + " / " + d.dir : ds;
  }

  function dsName(id) {
    var opt = qs('#dm-datasource option[value="' + id + '"]');
    return opt ? opt.textContent.replace(/\s*\(.*\)$/, "") : String(id);
  }

  function destFormatLabel(d) {
    if (d.kind === "rdbms") return d.dbMode;
    if (d.format === "dsv") return "DSV " + ({ comma: ",", tab: "\\t", pipe: "|", semicolon: ";" }[d.delimiter] || ",");
    if (d.format === "xml") return "XML";
    return "JSON " + d.jsonMode;
  }

  // ---- destination editor (modal) ----
  function openDest(index) {
    editingIndex = (typeof index === "number") ? index : -1;
    var d = editingIndex >= 0 ? dests[editingIndex] : newDest();
    qs("#dest-modal-title").textContent = editingIndex >= 0 ? "Edit destination" : "Add destination";
    qs("#dm-name").value = d.name;
    qs("#dm-kind").value = d.kind;
    qs("#dm-datasource").value = d.datasource;
    qs("#dm-bucket").value = d.bucket;
    qs("#dm-dir").value = d.dir;
    qs("#dm-delimiter").value = d.delimiter;
    qs("#dm-has-header").value = d.hasHeader;
    qs("#dm-json-mode").value = d.jsonMode;
    qs("#dm-xml-root").value = d.xmlRoot;
    qs("#dm-xml-record").value = d.xmlRecord;
    qs("#dm-compress").value = d.compress;
    qs("#dm-table").value = d.table;
    qs("#dm-db-mode").value = d.dbMode;
    var fr = qs('#dm-fmt-seg input[value="' + d.format + '"]');
    if (fr) { fr.checked = true; dmFormat(d.format); }
    dmKind(d.kind);
    dmDatasource();
    qs("#dm-passthrough").checked = d.passThrough;
    buildFields(d.fields);
    dmPassthrough(d.passThrough);
    qs("#dest-modal").hidden = false;
    qs("#dm-name").focus();
  }

  function closeDest() {
    qs("#dest-modal").hidden = true;
    editingIndex = -1;
  }

  // draftDest is the destination as currently entered in the editor, uncommitted —
  // used to preview the destination you are editing before you save it.
  function draftDest() {
    var fmtRadio = qs("#dm-fmt-seg input:checked");
    return {
      name: qs("#dm-name").value.trim() || "(unnamed)",
      kind: qs("#dm-kind").value,
      datasource: qs("#dm-datasource").value,
      bucket: qs("#dm-bucket").value.trim(),
      dir: qs("#dm-dir").value.trim(),
      format: fmtRadio ? fmtRadio.value : "json",
      delimiter: qs("#dm-delimiter").value,
      hasHeader: qs("#dm-has-header").value,
      jsonMode: qs("#dm-json-mode").value,
      xmlRoot: qs("#dm-xml-root").value.trim(),
      xmlRecord: qs("#dm-xml-record").value.trim(),
      passThrough: qs("#dm-passthrough").checked,
      fields: readFields(),
      compress: qs("#dm-compress").value,
      table: qs("#dm-table").value.trim(),
      dbMode: qs("#dm-db-mode").value,
    };
  }

  function saveDest() {
    var name = qs("#dm-name").value.trim();
    if (!name) { qs("#dm-name").focus(); flashInvalid(qs("#dm-name")); return; }
    var dup = dests.some(function (x, i) {
      return i !== editingIndex && x.name.toLowerCase() === name.toLowerCase();
    });
    if (dup) { flashInvalid(qs("#dm-name")); return; }

    var d = draftDest();
    d.name = name;
    if (editingIndex >= 0) dests[editingIndex] = d; else dests.push(d);
    closeDest();
    renderDests();
  }

  function flashInvalid(el) {
    if (!el) return;
    el.classList.add("invalid");
    setTimeout(function () { el.classList.remove("invalid"); }, 1200);
  }

  function dmKind(kind) {
    qsa("[data-dm-kind]").forEach(function (b) {
      b.hidden = b.getAttribute("data-dm-kind") !== kind;
    });
  }

  function dmFormat(fmt) {
    qsa("[data-dm-fmt]").forEach(function (b) { b.hidden = b.getAttribute("data-dm-fmt") !== fmt; });
    qsa("#dm-fmt-seg .segbtn").forEach(function (b) { b.classList.remove("on"); });
    var r = qs('#dm-fmt-seg input[value="' + fmt + '"]');
    if (r && r.closest(".segbtn")) r.closest(".segbtn").classList.add("on");
  }

  function dmDatasource() {
    var sel = qs("#dm-datasource");
    if (!sel) return;
    var opt = sel.options[sel.selectedIndex];
    var isS3 = opt && opt.getAttribute("data-kind") === "s3";
    var w = qs("#dm-bucket-wrap");
    if (w) w.hidden = !isS3;
    var conn = qs("#dm-ds-conn");
    if (conn) conn.textContent = opt ? (opt.getAttribute("data-conn") || "") : "";
  }

  // The field table is built from the DECLARED INPUT FIELDS (step 2). There is no
  // single global output structure any more — each destination populates its own.
  function dmPassthrough(on) {
    var w = qs("#dm-fields-wrap");
    if (w) w.hidden = !!on;
  }

  function buildFields(rows) {
    var host = qs("#dm-fields");
    if (!host) return;
    host.innerHTML = "";
    (rows || []).forEach(addFieldRow);
    syncFieldsTable();
  }

  function addFieldRow(row) {
    var tpl = qs("#dm-field-tpl"), host = qs("#dm-fields");
    if (!tpl || !host) return;
    var tr = tpl.content.firstElementChild.cloneNode(true);

    // Source is a picker over the declared input fields, so a destination can only
    // draw from fields the records actually carry.
    var sel = qs(".f-source", tr);
    var names = declaredNames();
    if (!names.length) {
      sel.appendChild(new Option("— declare input fields first (step 2) —", ""));
    }
    names.forEach(function (n) { sel.appendChild(new Option(n, n)); });

    if (row) {
      qs(".f-out", tr).value = row.output || "";
      qs(".f-kind", tr).value = row.kind || "field";
      qs(".f-type", tr).value = row.type || "";
      if (row.kind === "concat") qs(".f-parts", tr).value = row.value || "";
      else if (row.kind === "const") qs(".f-const", tr).value = row.value || "";
      else sel.value = row.source || row.output || "";
    }
    host.appendChild(tr);
    syncFieldRow(qs(".f-kind", tr));
    syncFieldsTable();
  }

  // Add one row per declared input field — the common case in one click.
  function addAllFields() {
    var host = qs("#dm-fields");
    if (!host) return;
    var have = {};
    qsa("#dm-fields tr").forEach(function (tr) {
      var o = qs(".f-out", tr).value.trim();
      if (o) have[o] = true;
    });
    declaredNames().forEach(function (n) {
      if (have[n]) return;
      addFieldRow({ output: n, kind: "field", source: n, type: declaredType(n) });
    });
  }

  function declaredType(name) {
    var t = "";
    qsa("#decl-rows tr").forEach(function (tr) {
      var n = qs('input[name="decl_name"]', tr);
      var ty = qs('select[name="decl_type"]', tr);
      if (n && n.value.trim() === name && ty) t = ty.value;
    });
    return t;
  }

  function syncFieldRow(kindSel) {
    var tr = kindSel.closest("tr");
    var k = kindSel.value;
    qs(".f-source", tr).hidden = k !== "field";
    qs(".f-const", tr).hidden = k !== "const";
    qs(".f-parts", tr).hidden = k !== "concat";
  }

  // Choosing an input field names the output field after it, unless the operator
  // has already renamed it — the rename case is exactly why this is a table.
  function fieldSourcePicked(sel) {
    var tr = sel.closest("tr");
    var out = qs(".f-out", tr);
    if (out && !out.value.trim()) out.value = sel.value;
    var ty = qs(".f-type", tr);
    if (ty && !ty.value) ty.value = declaredType(sel.value);
  }

  function moveFieldRow(btn, delta) {
    var tr = btn.closest("tr"), host = qs("#dm-fields");
    if (!tr || !host) return;
    var rows = Array.prototype.slice.call(host.children);
    var i = rows.indexOf(tr), j = i + delta;
    if (j < 0 || j >= rows.length) return;
    if (delta < 0) host.insertBefore(tr, rows[j]);
    else host.insertBefore(rows[j], tr);
  }

  function syncFieldsTable() {
    var host = qs("#dm-fields");
    var tbl = qs(".dm-fields-tbl");
    var empty = qs("#dm-fields-empty");
    if (!host) return;
    var any = host.children.length > 0;
    if (tbl) tbl.hidden = !any;
    if (empty) empty.hidden = any;
  }

  function readFields() {
    return qsa("#dm-fields tr").map(function (tr) {
      var kind = qs(".f-kind", tr).value;
      var value = "";
      if (kind === "const") value = qs(".f-const", tr).value.trim();
      else if (kind === "concat") value = qs(".f-parts", tr).value.trim();
      return {
        output: qs(".f-out", tr).value.trim(),
        kind: kind,
        source: qs(".f-source", tr).value,
        value: value,
        type: qs(".f-type", tr).value,
      };
    }).filter(function (f) { return f.output; });
  }

  // ================= change delegation =================
  document.addEventListener("change", function (e) {
    var el = e.target.closest("[data-action]");
    if (el) {
      var action = el.getAttribute("data-action");
      if (action === "kind") toggleFormat(el.getAttribute("data-target"), el.value);
      else if (action === "backend") toggleBackend(el.value);
      else if (action === "post-fetch") toggleAttr("postfetch", el.value === "move" ? "move" : "__none__");
      else if (action === "archive-toggle") { var b = qs("#archive-body"); if (b) b.hidden = !el.checked; }
      else if (action === "container-toggle") { var co = qs("#container-opts"); if (co) co.hidden = !el.checked; }
      else if (action === "archive-backend") toggleAttr("archdest", el.value);
      else if (action === "ds-kind") toggleAttr("dskind", el.value);
      else if (action === "batch-toggle") { var bo = qs("#batch-opts"); if (bo) bo.hidden = !el.checked; }
      else if (action === "dm-passthrough") dmPassthrough(el.checked);
      else if (action === "dm-field-kind") syncFieldRow(el);
      else if (action === "dm-field-source") fieldSourcePicked(el);
    }
    if (e.target && e.target.id === "dm-kind") dmKind(e.target.value);
    if (e.target && e.target.id === "dm-datasource") dmDatasource();
    if (e.target && e.target.name === "dm_format") dmFormat(e.target.value);
    // keep the output source dropdowns in sync as declared names are typed
  });
  document.addEventListener("input", function (e) {
    if (e.target && e.target.getAttribute && e.target.getAttribute("data-action") === "ds-search") filterDsOpts(e.target.value);
    clearStepError(stepSection(currentStep())); // dismiss the message as the user fixes it
  });

  function toggleFormat(target, kind) {
    qsa('[data-seg="' + target + '"] .segbtn').forEach(function (b) {
      b.classList.toggle("on", b.querySelector("input").value === kind);
    });
    qsa('[data-fmt^="' + target + '-"]').forEach(function (p) {
      p.hidden = p.getAttribute("data-fmt") !== target + "-" + kind;
    });
  }
  function toggleBackend(kind) {
    qsa('[data-seg="backend"] .segbtn').forEach(function (b) {
      b.classList.toggle("on", b.querySelector("input").value === kind);
    });
    qsa("[data-backend]").forEach(function (p) {
      p.hidden = p.getAttribute("data-backend") !== kind;
    });
  }

  // ---- datasource picker (searchable modal, shared by input + output) ----
  function openDsPicker(target) {
    var m = qs("#ds-modal");
    if (!m) return;
    m.setAttribute("data-target", target || "input");
    m.hidden = false;
    var s = qs(".ds-modal-search", m);
    if (s) { s.value = ""; filterDsOpts(""); s.focus(); }
  }
  function closeDsPicker() { var m = qs("#ds-modal"); if (m) m.hidden = true; }

  function filterDsOpts(q) {
    q = (q || "").toLowerCase();
    qsa("#ds-modal .ds-opt").forEach(function (li) {
      var name = (li.getAttribute("data-name") || "").toLowerCase();
      var conn = (li.getAttribute("data-conn") || "").toLowerCase();
      li.hidden = q !== "" && name.indexOf(q) < 0 && conn.indexOf(q) < 0;
    });
  }

  // Apply a chosen datasource to whichever selector (input/output) opened the modal:
  // set the hidden id (+ data-kind for validation), the button label, the read-only
  // connection line, and reveal the per-type bucket field for s3.
  function chooseDs(li) {
    var m = qs("#ds-modal");
    var target = m ? m.getAttribute("data-target") : "input";
    var id = li.getAttribute("data-id"), kind = li.getAttribute("data-kind");
    var name = li.getAttribute("data-name"), conn = li.getAttribute("data-conn") || "";
    var hidden = qs(target === "output" ? '[name="output_datasource_id"]' : '[name="datasource_id"]');
    if (hidden) { hidden.value = id; hidden.setAttribute("data-kind", kind); }
    var label = qs('[data-ds-label="' + target + '"]');
    if (label) label.textContent = name + " (" + kind.toUpperCase() + ")";
    var detail = qs('[data-ds-detail="' + target + '"]');
    if (detail) detail.hidden = false;
    var connEl = qs('[data-ds-conn="' + target + '"]');
    if (connEl) connEl.textContent = conn ? ("Connection: " + conn) : "";
    var bucket = qs(target === "output" ? "[data-ds-output-bucket]" : "[data-ds-bucket]");
    if (bucket) bucket.hidden = kind !== "s3";
    closeDsPicker();
    clearStepError(stepSection(1));
  }

  // Reset a selector back to unselected (input) / "same as input" (output).
  function clearDs(target) {
    var hidden = qs(target === "output" ? '[name="output_datasource_id"]' : '[name="datasource_id"]');
    if (hidden) { hidden.value = ""; hidden.removeAttribute("data-kind"); }
    var label = qs('[data-ds-label="' + target + '"]');
    if (label) label.textContent = target === "output" ? "Same as input datasource" : "Choose a datasource…";
    var detail = qs('[data-ds-detail="' + target + '"]');
    if (detail) detail.hidden = true;
  }

  // Test a datasource connection: post the enclosing form to its data-url and swap
  // the pass/fail fragment into data-target (no full-page reload).
  function testConnection(btn) {
    var form = btn.closest("form");
    var target = qs(btn.getAttribute("data-target") || "#test-out");
    if (!form || !target) return;
    target.innerHTML = '<div class="muted">Testing…</div>';
    fetch(btn.getAttribute("data-url"), { method: "POST", body: new URLSearchParams(new FormData(form)) })
      .then(function (r) { return r.text(); })
      .then(function (html) { target.innerHTML = html; })
      .catch(function (err) { target.innerHTML = '<div class="result-err">' + err + "</div>"; });
  }
  // Generic single-attribute show/hide (post-fetch move target, archive dest).
  function toggleAttr(attr, val) {
    qsa("[data-" + attr + "]").forEach(function (p) {
      p.hidden = p.getAttribute("data-" + attr) !== val;
    });
  }



  // Extract declared fields from the pasted sample: DSV header row or JSON keys.
  function parseFieldsFromSample() {
    var ta = qs("#sample");
    if (!ta || !ta.value.trim()) { return; }
    var kindEl = qs('input[name="input_kind"]:checked');
    var kind = kindEl ? kindEl.value : "dsv";
    var names = [];
    if (kind === "xml") {
      // Parse the sample and take the record element's attribute + child names.
      try {
        var doc = new DOMParser().parseFromString(ta.value, "application/xml");
        if (!doc.querySelector("parsererror")) {
          var recName = fval("input_xml_record");
          var recEl = recName ? doc.getElementsByTagName(recName)[0]
                              : (doc.documentElement && doc.documentElement.firstElementChild);
          if (recEl) {
            for (var ai = 0; ai < recEl.attributes.length; ai++) names.push(recEl.attributes[ai].name);
            for (var ch = recEl.firstElementChild; ch; ch = ch.nextElementSibling) {
              if (names.indexOf(ch.tagName) < 0) names.push(ch.tagName);
            }
          }
        }
      } catch (err) { /* leave names empty */ }
    } else if (kind === "json") {
      var line = ta.value.split(/\r?\n/).map(function (l) { return l.trim(); }).filter(Boolean)[0] || "";
      // handle a leading array bracket
      if (line.charAt(0) === "[") line = line.slice(1).trim();
      try {
        var obj = JSON.parse(line);
        if (obj && typeof obj === "object") names = Object.keys(obj);
      } catch (err) { /* leave names empty */ }
    } else {
      var delSel = qs('select[name="input_delimiter"]');
      var delim = delimFor(delSel ? delSel.value : "comma");
      var header = ta.value.split(/\r?\n/)[0] || "";
      names = header.split(delim).map(function (s) { return s.trim(); }).filter(Boolean);
    }
    if (names.length === 0) return;
    var rows = qs("#decl-rows");
    if (rows) rows.innerHTML = "";
    names.forEach(function (n) {
      addDeclRow();
      var last = qs("#decl-rows tr:last-child input[name='decl_name']");
      if (last) last.value = n;
    });
  }
  function delimFor(v) {
    switch (v) {
      case "tab": return "\t";
      case "pipe": return "|";
      case "semicolon": return ";";
      default: return ",";
    }
  }

  // --- live preview: post the builder form (urlencoded, no files) to /preview ---
  function runPreview() {
    var form = qs("#builder");
    var out = qs("#preview-out");
    if (!form || !out) return;
    out.innerHTML = '<div class="muted center">Running…</div>';
    var body = new URLSearchParams(new FormData(form));
    // Shape lives on the destination, so a preview previews ONE destination. While
    // the editor is open, preview the destination being edited — that is what makes
    // the field picker and format choices legible before you commit to them.
    var modal = qs("#dest-modal");
    if (modal && !modal.hidden) {
      body.delete("dest_json");
      body.append("dest_json", destJSON(draftDest()));
    }
    var lbl = qs("#preview-dest");
    if (lbl) {
      var d = (modal && !modal.hidden) ? draftDest() : dests[0];
      lbl.textContent = d && d.name ? "destination: " + d.name : "";
    }
    fetch("/preview", { method: "POST", body: body })
      .then(function (r) { return r.text(); })
      .then(function (html) { out.innerHTML = html; })
      .catch(function (err) { out.innerHTML = '<div class="result-err">' + err + "</div>"; });
  }

  // --- guard the wizard: on Create, jump to the first step missing required info ---
  document.addEventListener("submit", function (e) {
    if (!e.target || e.target.id !== "builder") return;
    for (var s = 1; s <= 4; s++) {
      var err = validateStep(s);
      if (err) { e.preventDefault(); showStep(s); showStepError(stepSection(s), err); return; }
    }
  });

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
    } else if (kind === "xml") {
      ta.value = '<?xml version="1.0"?>\n<cdrs>\n' +
                 '  <cdr><msisdn>27831234567</msisdn><seconds>90</seconds><cell>JHB-012</cell><dropped>false</dropped></cdr>\n' +
                 '  <cdr><msisdn>27829998888</msisdn><seconds>30</seconds><cell>CPT-104</cell><dropped>false</dropped></cdr>\n' +
                 '</cdrs>\n';
      setKind("input", "xml");
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
