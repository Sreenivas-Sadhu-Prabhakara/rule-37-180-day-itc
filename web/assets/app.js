/* Rule 37 guard — interface logic. Talks only to this binary, on this machine,
   over fetch to its own origin. Nothing else is contacted. */
(function () {
  "use strict";

  /* ---------------------------------------------------------- small helpers */

  var root = document.documentElement;
  function $(id) { return document.getElementById(id); }

  var inr = new Intl.NumberFormat("en-IN", {
    style: "currency", currency: "INR",
    minimumFractionDigits: 2, maximumFractionDigits: 2
  });
  function money(paise) {
    var n = Number(paise);
    if (!isFinite(n)) { n = 0; }
    return inr.format(n / 100);
  }
  function heads(h) { return money(h ? (h.igst + h.cgst + h.sgst + h.cess) : 0); }
  function total(h) { return h ? (h.igst + h.cgst + h.sgst + h.cess) : 0; }
  function dateText(iso) {
    if (!iso) { return "—"; }
    var m = /^(\d{4})-(\d{2})-(\d{2})/.exec(String(iso));
    return m ? m[3] + "-" + m[2] + "-" + m[1] : String(iso);
  }
  function el(tag, cls, text) {
    var n = document.createElement(tag);
    if (cls) { n.className = cls; }
    if (text !== undefined && text !== null) { n.textContent = String(text); }
    return n;
  }
  function clear(node) { while (node.firstChild) { node.removeChild(node.firstChild); } }
  function count(n, one, many) { return n + " " + (n === 1 ? one : many); }
  function show(node, on) { if (node) { node.hidden = !on; } }

  /* ----------------------------------------------------------- theme toggle */

  function currentTheme() {
    var t = root.getAttribute("data-theme");
    if (t) { return t; }
    return (window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches) ? "dark" : "light";
  }
  var themer = document.querySelector("[data-themer]");
  function paintThemer() {
    if (!themer) { return; }
    var dark = currentTheme() === "dark";
    themer.textContent = dark ? "☀ Light" : "☾ Dark";
    themer.setAttribute("aria-label", dark ? "Switch to light theme" : "Switch to dark theme");
  }
  if (themer) {
    paintThemer();
    themer.addEventListener("click", function () {
      var next = currentTheme() === "dark" ? "light" : "dark";
      root.setAttribute("data-theme", next);
      try { localStorage.setItem("omsme-theme", next); } catch (e) { /* storage may be blocked */ }
      paintThemer();
    });
  }

  /* ---------------------------------------------------------------- the API */

  function api(path, options) {
    return fetch(path, options).then(function (res) {
      var type = res.headers.get("content-type") || "";
      if (type.indexOf("application/json") < 0) {
        if (!res.ok) { throw new Error("the tool replied with status " + res.status); }
        return res.text();
      }
      return res.json().then(function (body) {
        if (!res.ok) { throw new Error((body && body.error) || ("status " + res.status)); }
        return body;
      });
    });
  }

  /* -------------------------------------------------------------- the state */

  var state = null;
  // reference holds the statutory figures and the FORM GSTR-3B table numbers,
  // read once from the binary so the interface never hard-codes them.
  var reference = { tableReversal: "4(B)(2)", tableReclaim: "4(A)(5)", tableReclaimDisclosure: "4(D)(1)" };
  var stageFilter = "all";
  var selectedID = "";

  // What the last import had to say, kept so that it can be shown beside the
  // flags the assessment raises rather than being overwritten by them.
  var importNotes = [];
  var importProblems = [];

  function query() {
    var parts = [];
    var asOf = $("i-asof").value;
    if (asOf) { parts.push("asOf=" + encodeURIComponent(asOf)); }
    var budget = $("i-budget").value.trim();
    if (budget) { parts.push("budget=" + encodeURIComponent(budget)); }
    return parts.length ? "?" + parts.join("&") : "";
  }

  function load() {
    return api("/api/state" + query()).then(render).catch(function (err) {
      fail($("import-error"), err.message);
    });
  }

  function fail(box, message) {
    if (!box) { return; }
    box.textContent = message;
    show(box, true);
  }
  function quiet(box) { if (box) { box.textContent = ""; show(box, false); } }

  /* ----------------------------------------------------------- the settings */

  var DUE_DAYS = [20, 22, 24];
  function fillDueDays() {
    var sel = $("i-dueday");
    clear(sel);
    DUE_DAYS.forEach(function (d) {
      var label = d + (d === 20 ? "th — monthly filers" : "nd — a quarterly filer in one group of States");
      if (d === 24) { label = "24th — a quarterly filer in the other group"; }
      sel.appendChild(new Option(label, String(d)));
    });
  }

  function paintSettings(s, business) {
    $("i-period").value = String(s.returnPeriodMonths);
    $("i-dueday").value = String(s.returnDueDay);
    $("i-rate").value = String(s.interestRatePct);
    $("i-basis").value = s.interestBasis;
    $("i-window").value = String(s.paymentWindowDays);
    $("i-horizon").value = String(s.planHorizonDays);
    $("i-business").value = business.name || "";
    $("i-gstin").value = business.gstin || "";
  }

  function saveSettings(settings) {
    var body = {
      business: { name: $("i-business").value, gstin: $("i-gstin").value },
      settings: settings
    };
    return api("/api/settings" + query(), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body)
    }).then(function (next) {
      render(next);
      $("settings-status").textContent = "Saved.";
    }).catch(function (err) {
      $("settings-status").textContent = err.message;
    });
  }

  $("go-settings").addEventListener("click", function () {
    saveSettings({
      paymentWindowDays: Number($("i-window").value) || 0,
      returnPeriodMonths: Number($("i-period").value) || 1,
      returnDueDay: Number($("i-dueday").value) || 0,
      interestRatePct: Number($("i-rate").value) || 0,
      interestBasis: $("i-basis").value,
      bucketEdges: state ? state.settings.bucketEdges : null,
      planHorizonDays: Number($("i-horizon").value) || 0
    });
  });

  $("go-defaults").addEventListener("click", function () {
    if (!state) { return; }
    paintSettings(state.defaults, { name: $("i-business").value, gstin: $("i-gstin").value });
    saveSettings(state.defaults);
  });

  // Filing monthly or quarterly changes which due days make sense.
  $("i-period").addEventListener("change", function () {
    var quarterly = $("i-period").value === "3";
    $("i-dueday").value = quarterly ? "22" : "20";
  });

  /* ------------------------------------------------------------- the import */

  function importFiles() {
    quiet($("import-error"));
    var status = $("import-status");
    var reg = $("f-register").files[0];
    var pay = $("f-payments").files[0];
    var regPath = $("p-register").value.trim();
    var payPath = $("p-payments").value.trim();
    var replace = $("i-replace").checked;

    var options;
    if (reg || pay) {
      var form = new FormData();
      if (reg) { form.append("register", reg); }
      if (pay) { form.append("payments", pay); }
      form.append("replace", replace ? "true" : "false");
      options = { method: "POST", body: form };
    } else if (regPath || payPath) {
      options = {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ registerPath: regPath, paymentsPath: payPath, replace: replace })
      };
    } else {
      fail($("import-error"), "Choose a purchase register, or give the path to one on this computer.");
      return;
    }

    status.textContent = "Reading…";
    document.body.classList.add("busy");
    api("/api/import" + query(), options).then(function (out) {
      status.textContent = count(out.imported, "invoice", "invoices") + " read" +
        (out.replaced ? ", replacing what was loaded before." : ", added to what was already loaded.");
      keepImportNotes(out);
      render(out.state);
    }).catch(function (err) {
      status.textContent = "Nothing was read.";
      fail($("import-error"), err.message);
    }).then(function () {
      document.body.classList.remove("busy");
    });
  }
  $("go-import").addEventListener("click", importFiles);

  $("go-clear").addEventListener("click", function () {
    quiet($("import-error"));
    api("/api/invoices" + query(), { method: "DELETE" }).then(function (next) {
      $("import-status").textContent = "Cleared. The data file now holds no invoices.";
      importNotes = [];
      importProblems = [];
      render(next);
    }).catch(function (err) { fail($("import-error"), err.message); });
  });

  function keepImportNotes(out) {
    importNotes = [];
    importProblems = [];
    (out.register.notes || []).forEach(function (n) { importNotes.push(n); });
    (out.payments.notes || []).forEach(function (n) { importNotes.push(n); });
    (out.register.problems || []).forEach(function (p) { importProblems.push(["purchase register", p]); });
    (out.payments.problems || []).forEach(function (p) { importProblems.push(["payments file", p]); });
    (out.attached || []).forEach(function (p) { importProblems.push(["payments file", p]); });
  }

  function renderNotes(summary) {
    var notes = $("notes");
    var problems = $("problems");
    clear(notes);
    clear(problems);

    importNotes.forEach(function (n) { notes.appendChild(el("p", "hintline", n)); });
    (summary.flags || []).forEach(function (f) { notes.appendChild(el("p", "hintline", f)); });

    if (importProblems.length) {
      problems.appendChild(el("p", "hintline",
        count(importProblems.length, "row was", "rows were") + " left out. Each is named by its line so you can fix it and read the file again."));
      var ul = el("ul", "problems");
      importProblems.forEach(function (pair) {
        var p = pair[1];
        var li = el("li");
        li.appendChild(el("code", null, pair[0] + " line " + p.line));
        li.appendChild(document.createTextNode(" — " + (p.field ? p.field + ": " : "") + p.reason));
        ul.appendChild(li);
      });
      problems.appendChild(ul);
    }
    show($("notes-card"), notes.childNodes.length > 0 || importProblems.length > 0);
  }

  /* ------------------------------------------------------------ the headline */

  function renderHeadline(s) {
    $("r-atrisk").textContent = heads(s.atRisk);
    $("r-thisperiod").textContent = heads(s.riskThisPeriod);
    $("r-reversal").textContent = heads(s.reversalDue);
    $("r-overdue").textContent = heads(s.reversalOverdue);
    $("r-reclaim").textContent = heads(s.reclaimAvailable);
    $("r-interest").textContent = money(s.interest);
    $("r-net").textContent = money(total(s.reversalDue) - total(s.reclaimAvailable));

    var sub = $("r-sub");
    if (!s.invoices) {
      sub.textContent = "Load a purchase register to see where you stand";
    } else if (total(s.atRisk) > 0) {
      sub.textContent = "across " + count(s.atRiskInvoices, "invoice", "invoices") +
        ", protected by paying " + money(s.cashToProtectAll) + " before their deadlines";
    } else {
      sub.textContent = "nothing left to protect — every open invoice is paid, or its deadline has gone";
    }

    var counts = [];
    counts.push(count(s.invoices, "invoice", "invoices") + " loaded");
    if (s.counted && s.counted.settled) { counts.push(s.counted.settled + " paid in time"); }
    if (s.excluded) { counts.push(s.excluded + " out of scope"); }
    if (s.unusable) { counts.push(s.unusable + " could not be assessed"); }
    counts.push("figures as at " + dateText(s.asOf) + ", in the " + periodLabel(s.thisPeriod) + " return period");
    $("r-counts").textContent = counts.join(" · ");
  }

  function periodName(p) {
    if (!p || !p.month) { return "—"; }
    var names = ["January", "February", "March", "April", "May", "June",
      "July", "August", "September", "October", "November", "December"];
    if (p.months === 3) {
      var end = p.month + 2;
      return names[p.month - 1] + "–" + names[(end - 1) % 12];
    }
    return names[p.month - 1];
  }
  function periodLabel(p) {
    if (!p || !p.month) { return "—"; }
    return periodName(p) + " " + p.year;
  }

  /* ---------------------------------------------------------------- the plan */

  function renderPlan(plan) {
    var stats = $("plan-stats");
    var body = $("plan-body");
    clear(stats);
    clear(body);
    show($("plan-card"), plan.actions && plan.actions.length > 0);
    if (!plan.actions || !plan.actions.length) { return; }

    stat(stats, "Credit the plan saves", heads(plan.protected), plan.limited ? "for " + money(plan.cashUsed) + " of cash" : "paying everything in the horizon", "ok");
    stat(stats, "Cash needed in full", money(plan.cashNeeded), count(plan.invoices, "invoice", "invoices") + " inside the horizon", "");
    if (plan.limited) {
      stat(stats, "Still short", money(Math.max(plan.cashNeeded - plan.cashUsed, 0)), "to protect every rupee of credit", "");
      stat(stats, "Credit lost anyway", heads(plan.stillAtRisk), "if no more cash is found", total(plan.stillAtRisk) > 0 ? "bad" : "ok");
    }

    plan.actions.forEach(function (a, i) {
      var tr = el("tr");
      cell(tr, String(i + 1), "num");
      cell(tr, a.supplierName || a.supplierGstin || "—", "");
      cell(tr, a.invoiceNo, "mono");
      cell(tr, dateText(a.deadline), "num nowrap");
      cell(tr, String(a.daysLeft), "num" + (a.daysLeft <= 15 ? " bad" : a.daysLeft <= 30 ? " risk" : ""));
      cell(tr, money(a.unpaid), "num");
      cell(tr, a.pay > 0 ? money(a.pay) + (a.partial ? " (part)" : "") : "—", "num");
      cell(tr, heads(a.protected), "num" + (a.pay > 0 ? " good" : ""));
      cell(tr, String(a.protectedPerRupee), "num mono");
      body.appendChild(tr);
    });
  }

  function stat(into, k, v, s, kind) {
    var box = el("div", "stat" + (kind ? " " + kind : ""));
    box.appendChild(el("div", "k", k));
    box.appendChild(el("div", "v", v));
    if (s) { box.appendChild(el("div", "s", s)); }
    into.appendChild(box);
  }

  function cell(tr, text, cls) {
    tr.appendChild(el("td", cls || null, text));
    return tr;
  }

  /* ------------------------------------------------------------- the periods */

  function renderPeriods(s) {
    var body = $("periods-body");
    clear(body);
    show($("periods-card"), s.periods && s.periods.length > 0);
    if (!s.periods) { return; }

    s.periods.forEach(function (p) {
      var tr = el("tr");
      var name = periodLabel(p.period);
      if (p.period && s.thisPeriod && p.period.year === s.thisPeriod.year && p.period.month === s.thisPeriod.month) {
        name += " — this period";
      }
      cell(tr, name, "");
      cell(tr, dateText(p.dueDate) + (p.overdue ? " (passed)" : ""), "num nowrap" + (p.overdue ? " bad" : ""));
      cell(tr, total(p.reversal) ? heads(p.reversal) : "—", "num" + (total(p.reversal) ? " bad" : ""));
      cell(tr, total(p.projected) ? heads(p.projected) : "—", "num" + (total(p.projected) ? " risk" : ""));
      cell(tr, p.cashToAvoid ? money(p.cashToAvoid) : "—", "num");
      cell(tr, total(p.reclaim) ? heads(p.reclaim) : "—", "num good");
      cell(tr, money(total(p.reversal) - total(p.reclaim)), "num");
      body.appendChild(tr);
    });
  }

  /* -------------------------------------------------------------- the ageing */

  function renderAgeing(s) {
    var body = $("ageing-body");
    clear(body);
    var any = (s.buckets || []).some(function (b) { return b.invoices > 0; });
    show($("ageing-card"), any);
    if (!any) { return; }

    (s.buckets || []).forEach(function (b) {
      var tr = el("tr");
      cell(tr, b.bucket.label, "");
      cell(tr, b.invoices ? String(b.invoices) : "—", "num");
      cell(tr, b.unpaid ? money(b.unpaid) : "—", "num");
      cell(tr, total(b.atRisk) ? heads(b.atRisk) : "—", "num" + (total(b.atRisk) ? " risk" : ""));
      cell(tr, total(b.reversal) ? heads(b.reversal) : "—", "num" + (total(b.reversal) ? " bad" : ""));
      cell(tr, dateText(b.earliest), "num nowrap");
      cell(tr, dateText(b.latest), "num nowrap");
      body.appendChild(tr);
    });
  }

  /* ----------------------------------------------------------- the suppliers */

  function renderSuppliers(s) {
    var body = $("suppliers-body");
    clear(body);
    show($("suppliers-card"), s.suppliers && s.suppliers.length > 0);
    if (!s.suppliers) { return; }

    s.suppliers.forEach(function (sup) {
      var tr = el("tr");
      cell(tr, sup.name || "—", "");
      cell(tr, sup.gstin || "—", "mono");
      cell(tr, String(sup.invoices), "num");
      cell(tr, money(sup.unpaid), "num");
      cell(tr, total(sup.atRisk) ? heads(sup.atRisk) : "—", "num" + (total(sup.atRisk) ? " risk" : ""));
      cell(tr, total(sup.reversal) ? heads(sup.reversal) : "—", "num" + (total(sup.reversal) ? " bad" : ""));
      cell(tr, sup.nextDeadline ? dateText(sup.nextDeadline) + " (" + sup.daysToNext + "d)" : "—", "num nowrap");
      cell(tr, sup.protectedPerRupee ? String(sup.protectedPerRupee) : "—", "num mono");
      body.appendChild(tr);
    });
  }

  /* ----------------------------------------------------------- invoice lines */

  var STAGES = [
    { id: "all", label: "Everything", note: "" },
    { id: "watch", label: "Credit at risk", note: "Still inside the 180 days with something unpaid. Pay before the deadline and the credit is safe." },
    { id: "reverse", label: "Reverse", note: "The 180 days ran out with an amount unpaid. The credit must be reversed in the return shown, and still has to be paid." },
    { id: "reclaim", label: "Reverse and re-claim", note: "The 180 days ran out with an amount unpaid which has since been paid. Report the reversal and take the credit again." },
    { id: "settled", label: "Paid in time", note: "Paid in full inside the 180 days. Nothing to do." },
    { id: "excluded", label: "Out of scope", note: "The 180-day condition does not reach these, for the reason shown against each." },
    { id: "unusable", label: "Cannot assess", note: "Something needed to put these on the clock is missing." }
  ];

  function renderLines(st) {
    var tabs = $("stage-tabs");
    var body = $("lines-body");
    clear(tabs);
    clear(body);
    show($("lines-card"), st.lines && st.lines.length > 0);
    if (!st.lines || !st.lines.length) { show($("rowdetail"), false); return; }

    var counts = {};
    st.lines.forEach(function (ln) {
      counts[ln.assessment.stage] = (counts[ln.assessment.stage] || 0) + 1;
    });

    STAGES.forEach(function (s) {
      var n = s.id === "all" ? st.lines.length : (counts[s.id] || 0);
      if (!n) { return; }
      var b = el("button", null, s.label);
      b.type = "button";
      b.setAttribute("aria-pressed", stageFilter === s.id ? "true" : "false");
      b.appendChild(el("span", "n", n));
      b.addEventListener("click", function () {
        stageFilter = s.id;
        renderLines(st);
      });
      tabs.appendChild(b);
    });

    var chosen = STAGES.filter(function (s) { return s.id === stageFilter; })[0] || STAGES[0];
    $("stage-note").textContent = chosen.note;

    var shown = st.lines.filter(function (ln) {
      return stageFilter === "all" || ln.assessment.stage === stageFilter;
    });
    shown.sort(function (a, b) {
      var x = a.assessment.deadline || "", y = b.assessment.deadline || "";
      return x < y ? -1 : x > y ? 1 : 0;
    });

    if (!shown.length) {
      var tr = el("tr");
      var td = el("td", "empty", "Nothing in this group.");
      td.colSpan = 11;
      tr.appendChild(td);
      body.appendChild(tr);
      show($("rowdetail"), false);
      return;
    }

    shown.forEach(function (ln) {
      var inv = ln.invoice, a = ln.assessment;
      var tr = el("tr", "rowpick");
      tr.setAttribute("aria-selected", inv.id === selectedID ? "true" : "false");
      cell(tr, inv.supplierName || inv.supplierGstin || "—", "");
      cell(tr, inv.invoiceNo, "mono");
      cell(tr, dateText(inv.invoiceDate), "num nowrap");
      cell(tr, dateText(a.deadline), "num nowrap");
      cell(tr, a.deadline ? (a.daysLeft >= 0 ? a.daysLeft + " left" : (-a.daysLeft) + " past") : "—",
        "num nowrap" + (a.daysLeft < 0 ? " bad" : a.daysLeft <= 30 ? " risk" : ""));
      cell(tr, money(a.payable), "num");
      cell(tr, money(a.paidToDate), "num");
      cell(tr, money(a.unpaidToDate), "num");
      cell(tr, total(a.atRisk) ? heads(a.atRisk) : "—", "num" + (total(a.atRisk) ? " risk" : ""));
      cell(tr, total(a.reversal) ? heads(a.reversal) : "—", "num" + (total(a.reversal) ? " bad" : ""));
      cell(tr, total(a.reversal) || total(a.atRisk) ? periodLabel(a.reversalPeriod) : "—", "nowrap");
      tr.addEventListener("click", function () {
        selectedID = selectedID === inv.id ? "" : inv.id;
        renderLines(st);
      });
      body.appendChild(tr);
    });

    renderDetail(st, shown);
  }

  function renderDetail(st, shown) {
    var box = $("rowdetail");
    clear(box);
    var picked = shown.filter(function (ln) { return ln.invoice.id === selectedID; })[0];
    show(box, !!picked);
    if (!picked) { return; }

    var inv = picked.invoice, a = picked.assessment;
    box.appendChild(el("h3", null, inv.invoiceNo + " — " + (inv.supplierName || inv.supplierGstin || "supplier not recorded")));
    box.appendChild(el("p", "why", a.reason));

    var tags = el("div", "tags");
    tags.appendChild(el("span", "tag", "day 180 on " + dateText(a.deadline)));
    if (total(a.reversal)) {
      tags.appendChild(el("span", "tag bad", "reverse " + heads(a.reversalRupees) + " in table " + reference.tableReversal + ", " + periodLabel(a.reversalPeriod) + " return"));
    }
    if (total(a.reclaim)) {
      tags.appendChild(el("span", "tag ok", "re-claim " + heads(a.reclaimRupees) + " in table " + reference.tableReclaim));
    }
    if (a.interest) {
      tags.appendChild(el("span", "tag warn", "indicative interest " + money(a.interest) + " over " + count(a.lateDays, "day", "days")));
    }
    if (a.exclusion) { tags.appendChild(el("span", "tag", "out of scope: " + a.exclusion)); }
    box.appendChild(tags);

    (a.flags || []).forEach(function (f) { box.appendChild(el("p", "hintline", f)); });

    var list = el("ul", "paylist");
    (inv.payments || []).forEach(function (p, i) {
      var li = el("li");
      li.appendChild(el("span", null, p.date ? dateText(p.date) : "date not recorded"));
      if (p.ref) { li.appendChild(el("span", "tag", p.ref)); }
      li.appendChild(el("span", "amt", money(p.amount)));
      var del = el("button", null, "Remove");
      del.type = "button";
      del.addEventListener("click", function (ev) {
        ev.stopPropagation();
        api("/api/invoices/" + encodeURIComponent(inv.id) + "/payments/" + i + query(), { method: "DELETE" })
          .then(render)
          .catch(function (err) { fail($("import-error"), err.message); });
      });
      li.appendChild(del);
      list.appendChild(li);
    });
    if (!(inv.payments || []).length) {
      list.appendChild(el("li", null, "Nothing recorded as paid against this invoice."));
    }
    box.appendChild(list);

    var form = el("div", "payform");
    var amtField = el("div", "field");
    var amtLabel = el("label", null, "Record a payment");
    amtLabel.htmlFor = "d-amount";
    amtField.appendChild(amtLabel);
    var amt = document.createElement("input");
    amt.type = "text";
    amt.id = "d-amount";
    amt.inputMode = "decimal";
    amt.placeholder = "50,000";
    amtField.appendChild(amt);
    form.appendChild(amtField);

    var dateField = el("div", "field");
    var dateLabel = el("label", null, "On");
    dateLabel.htmlFor = "d-date";
    dateField.appendChild(dateLabel);
    var when = document.createElement("input");
    when.type = "date";
    when.id = "d-date";
    dateField.appendChild(when);
    form.appendChild(dateField);

    var add = el("button", "btn primary", "Add");
    add.type = "button";
    add.addEventListener("click", function (ev) {
      ev.stopPropagation();
      api("/api/invoices/" + encodeURIComponent(inv.id) + "/payments" + query(), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ amount: paise(amt.value), date: when.value || "", ref: "" })
      }).then(render).catch(function (err) { fail($("import-error"), err.message); });
    });
    form.appendChild(add);
    box.appendChild(form);

    var row = el("div", "btnrow");
    var drop = el("button", "btn ghost", "Remove this invoice");
    drop.type = "button";
    drop.addEventListener("click", function (ev) {
      ev.stopPropagation();
      api("/api/invoices/" + encodeURIComponent(inv.id) + query(), { method: "DELETE" })
        .then(function (next) { selectedID = ""; render(next); })
        .catch(function (err) { fail($("import-error"), err.message); });
    });
    row.appendChild(drop);
    box.appendChild(row);
  }

  /* ------------------------------------------------------- the working paper */

  function paperQuery() {
    var parts = [];
    var asOf = $("i-asof").value;
    if (asOf) { parts.push("asOf=" + encodeURIComponent(asOf)); }
    var period = $("i-paper-period").value;
    if (period) { parts.push("period=" + encodeURIComponent(period)); }
    return parts.length ? "?" + parts.join("&") : "";
  }

  function loadPaper() {
    if (!state || !state.lines || !state.lines.length) {
      show($("paper-card"), false);
      return;
    }
    show($("paper-card"), true);
    $("x-paper").href = "/api/workingpaper" + paperQuery() + (paperQuery() ? "&" : "?") + "download=true";
    api("/api/workingpaper" + paperQuery()).then(function (text) {
      $("paper").textContent = text;
    }).catch(function (err) {
      $("paper").textContent = err.message;
    });
  }
  $("i-paper-period").addEventListener("change", loadPaper);

  $("go-copy").addEventListener("click", function () {
    var text = $("paper").textContent;
    var status = $("copy-status");
    if (!navigator.clipboard) {
      status.textContent = "Select the text above and copy it.";
      return;
    }
    navigator.clipboard.writeText(text).then(function () {
      status.textContent = "Copied. Paste it wherever your working papers live.";
    }).catch(function () {
      status.textContent = "Could not copy. Select the text above instead.";
    });
  });

  /* --------------------------------------------------------- adding by hand */

  function paise(text) {
    var t = String(text || "").replace(/[₹,\s]/g, "");
    if (!t) { return 0; }
    var n = Number(t);
    if (!isFinite(n)) { return 0; }
    return Math.round(n * 100);
  }

  $("go-add").addEventListener("click", function () {
    quiet($("add-error"));
    var payments = [];
    var paid = paise($("n-paid").value);
    if (paid > 0) { payments.push({ amount: paid, date: $("n-paiddate").value || "" }); }

    var body = {
      supplierName: $("n-supplier").value,
      supplierGstin: $("n-gstin").value,
      invoiceNo: $("n-invno").value,
      invoiceDate: $("n-invdate").value || "",
      taxableValue: paise($("n-taxable").value),
      itc: {
        igst: paise($("n-igst").value),
        cgst: paise($("n-cgst").value),
        sgst: paise($("n-sgst").value),
        cess: paise($("n-cess").value)
      },
      invoiceValue: paise($("n-total").value),
      payments: payments,
      exclusion: $("n-exclusion").value
    };

    api("/api/invoices" + query(), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body)
    }).then(function (next) {
      $("add-status").textContent = "Added.";
      ["n-invno", "n-taxable", "n-total", "n-igst", "n-cgst", "n-sgst", "n-cess", "n-paid"].forEach(function (id) {
        $(id).value = "";
      });
      render(next);
    }).catch(function (err) {
      $("add-status").textContent = "";
      fail($("add-error"), err.message);
    });
  });

  /* ------------------------------------------------------------- the exports */

  function paintExports() {
    var q = query();
    var join = q ? "&" : "?";
    [["x-watchlist", "watchlist"], ["x-reversals", "reversals"], ["x-reclaims", "reclaims"],
      ["x-all", "all"], ["x-plan", "plan"]].forEach(function (pair) {
      var node = $(pair[0]);
      if (node) { node.href = "/api/export" + q + join + "view=" + pair[1]; }
    });
  }

  /* --------------------------------------------------------------- rendering */

  function render(next) {
    state = next;
    if (!$("i-asof").value && next.today) { $("i-asof").value = next.today; }
    if (!$("i-paper-period").value && next.asOf) { $("i-paper-period").value = next.asOf; }
    paintSettings(next.settings, next.business);
    renderHeadline(next.summary);
    renderNotes(next.summary);
    renderPlan(next.plan);
    renderPeriods(next.summary);
    renderAgeing(next.summary);
    renderSuppliers(next.summary);
    renderLines(next);
    paintExports();
    loadPaper();
  }

  /* ------------------------------------------------------------------ wiring */

  ["i-asof", "i-budget"].forEach(function (id) {
    $(id).addEventListener("change", function () { load(); });
  });
  $("i-horizon").addEventListener("change", function () {
    if (!state) { return; }
    var s = JSON.parse(JSON.stringify(state.settings));
    s.planHorizonDays = Number($("i-horizon").value) || 0;
    saveSettings(s);
  });

  fillDueDays();
  api("/api/reference").then(function (ref) {
    reference = ref;
  }).catch(function () {
    // The built-in table numbers stand in; everything else still works.
  }).then(load);
})();
