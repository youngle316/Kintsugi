// The main window: overview (statistics), mistakes, the capture lab of
// step 0, settings, and the first-run guide.
import { Chart, ArcElement, BarController, BarElement, CategoryScale, DoughnutController, Legend, LinearScale, Tooltip } from "chart.js";
import { Kintsugi, events, type CaptureMode, type Filter, type LabRun, type Settings } from "./mygo";
import { $, categoryLabel, esc, errorText, formatTime } from "./util";

Chart.register(ArcElement, BarController, BarElement, CategoryScale, DoughnutController, Legend, LinearScale, Tooltip);

type Tab = "overview" | "mistakes" | "lab" | "settings" | "welcome";
const tabs: { id: Tab; label: string }[] = [
  { id: "overview", label: "Overview" },
  { id: "mistakes", label: "Mistakes" },
  { id: "lab", label: "Capture Lab" },
  { id: "settings", label: "Settings" },
];

let settings: Settings;
let tab: Tab = "overview";
const charts: Chart[] = [];

export async function start() {
  settings = await Kintsugi.settings();
  document.body.innerHTML = `
    <div class="app">
      <nav>
        <div class="brand"><span class="crack"></span>Kintsugi</div>
        ${tabs.map((t) => `<a href="#" data-tab="${t.id}">${t.label}</a>`).join("")}
        <span class="grow"></span>
        <div class="muted small" id="shortcut-hint"></div>
      </nav>
      <main id="view"></main>
    </div>`;
  document.querySelectorAll<HTMLElement>("nav a").forEach((a) => {
    a.onclick = (e) => {
      e.preventDefault();
      void show(a.dataset.tab as Tab);
    };
  });
  events.navigate.on((t) => void show(t as Tab));
  events.dataChanged.on(() => {
    if (tab === "overview" || tab === "mistakes") void show(tab);
  });
  events.labChanged.on(() => {
    if (tab === "lab") void show("lab");
  });
  const needsSetup = !settings.onboarded || !(await Kintsugi.hasAPIKey());
  await show(needsSetup ? "welcome" : "overview");
}

async function show(t: Tab) {
  tab = t;
  settings = await Kintsugi.settings();
  $("#shortcut-hint").textContent = `Check: ${pretty(settings.shortcut)}`;
  document.querySelectorAll<HTMLElement>("nav a").forEach((a) => a.classList.toggle("active", a.dataset.tab === t));
  charts.splice(0).forEach((c) => c.destroy());
  const view = $("#view");
  try {
    if (t === "overview") await overview(view);
    else if (t === "mistakes") await mistakes(view);
    else if (t === "lab") await lab(view);
    else if (t === "settings") await settingsView(view);
    else await welcome(view);
  } catch (err) {
    view.innerHTML = `<p class="error">${esc(errorText(err))}</p>`;
  }
}

/** Accelerators as macOS writes them: Alt+Cmd+G → ⌥⌘G. */
function pretty(accel: string): string {
  const sym: Record<string, string> = { ctrl: "⌃", control: "⌃", alt: "⌥", option: "⌥", shift: "⇧", cmd: "⌘", command: "⌘", cmdorctrl: "⌘" };
  const parts = accel.split("+");
  const key = parts.pop() ?? "";
  const order = ["⌃", "⌥", "⇧", "⌘"];
  const mods = parts.map((p) => sym[p.toLowerCase()] ?? p).sort((a, b) => order.indexOf(a) - order.indexOf(b));
  return mods.join("") + key.toUpperCase();
}

// ---- Overview ----

async function overview(view: HTMLElement) {
  const days = Number(sessionStorage.getItem("days") ?? 30);
  const o = await Kintsugi.overview(days);
  const lang = settings.language;
  view.innerHTML = `
    <div class="head"><h1>Overview</h1><span class="grow"></span>
      <select id="days">${[7, 30, 90, 0].map((d) => `<option value="${d}" ${d === days ? "selected" : ""}>${d ? `Last ${d} days` : "All time"}</option>`).join("")}</select></div>
    <div class="stats">
      <div class="stat"><b>${o.submissions}</b><span>checks</span></div>
      <div class="stat"><b>${o.words}</b><span>words</span></div>
      <div class="stat"><b>${o.errors}</b><span>errors</span></div>
      <div class="stat gold"><b>${o.errorsPer100.toFixed(2)}</b><span>errors / 100 words</span></div>
      <div class="stat"><b>${o.suggestions}</b><span>suggestions</span></div>
      <div class="stat"><b>${o.cleanChecks}</b><span>clean checks</span></div>
    </div>
    ${
      o.submissions === 0
        ? `<div class="empty">No checks yet. Select some English you wrote in any app and press <kbd>${pretty(settings.shortcut)}</kbd>.</div>`
        : `<div class="grid2">
            <section class="card"><h2>Mistakes by type</h2><div class="chart"><canvas id="cats"></canvas></div></section>
            <section class="card"><h2>Top 10 patterns <span class="muted small">last 30 days</span></h2>
              <ol class="top">${o.topPatterns
                .map((p) => `<li><div><a href="#" data-pattern="${esc(p.key)}">${esc(p.key)}</a><div class="muted small">${esc(categoryLabel(p.label ?? "", lang))}</div></div><b>${p.count}</b></li>`)
                .join("") || `<li class="muted">None yet</li>`}</ol></section>
          </div>
          <section class="card"><h2>By app</h2>${o.sourceApps.map((a) => `<span class="chip">${esc(a.label || a.key || "Clipboard")} · ${a.count}</span>`).join(" ")}</section>`
    }`;
  $<HTMLSelectElement>("#days").onchange = (e) => {
    sessionStorage.setItem("days", (e.target as HTMLSelectElement).value);
    void show("overview");
  };
  view.querySelectorAll<HTMLElement>("[data-pattern]").forEach((a) => {
    a.onclick = (e) => {
      e.preventDefault();
      sessionStorage.setItem("filter", JSON.stringify({ pattern: a.dataset.pattern }));
      void show("mistakes");
    };
  });
  const canvas = view.querySelector<HTMLCanvasElement>("#cats");
  if (canvas && o.categories.length) {
    const gold = ["#c9a227", "#b5651d", "#8c6d3f", "#d8b45a", "#6f5a2e", "#a3803a", "#e0c27a", "#7b4b1e", "#c48a3a", "#9e8a5a", "#5c4a28", "#d9a441", "#856e44", "#b89b5e"];
    charts.push(
      new Chart(canvas, {
        type: "doughnut",
        data: {
          labels: o.categories.map((c) => categoryLabel(c.key, lang)),
          datasets: [{ data: o.categories.map((c) => c.count), backgroundColor: gold, borderWidth: 0 }],
        },
        options: { maintainAspectRatio: false, plugins: { legend: { position: "bottom", labels: { color: getComputedStyle(document.body).color, boxWidth: 12 } } } },
      }),
    );
  }
}

// ---- Mistakes ----

async function mistakes(view: HTMLElement) {
  const f: Filter = { category: "", severity: "", sourceApp: "", since: 0, until: 0, pattern: "", limit: 200, offset: 0, ...JSON.parse(sessionStorage.getItem("filter") ?? "{}") };
  const [rows, cats, o] = await Promise.all([Kintsugi.issues(f), Kintsugi.categories(), Kintsugi.overview(0)]);
  const lang = settings.language;
  const sinceDays = f.since ? Math.round((Date.now() - f.since) / 86400000) : 0;
  view.innerHTML = `
    <div class="head"><h1>Mistakes</h1><span class="muted">${rows.length} shown</span></div>
    <div class="filters">
      <select id="f-category"><option value="">All types</option>${cats.map((c) => `<option value="${c}" ${c === f.category ? "selected" : ""}>${esc(categoryLabel(c, lang))}</option>`).join("")}</select>
      <select id="f-severity"><option value="">Errors & suggestions</option><option value="error" ${f.severity === "error" ? "selected" : ""}>Errors</option><option value="suggestion" ${f.severity === "suggestion" ? "selected" : ""}>Suggestions</option></select>
      <select id="f-app"><option value="">All apps</option>${o.sourceApps.map((a) => `<option value="${esc(a.key)}" ${a.key === f.sourceApp ? "selected" : ""}>${esc(a.label || a.key || "Clipboard")}</option>`).join("")}</select>
      <select id="f-since">${[0, 1, 7, 30, 90].map((d) => `<option value="${d}" ${d === sinceDays ? "selected" : ""}>${d ? `Last ${d} day${d > 1 ? "s" : ""}` : "Any time"}</option>`).join("")}</select>
      ${f.pattern ? `<span class="chip">pattern: ${esc(f.pattern)} <a href="#" id="f-clear-pattern">×</a></span>` : ""}
    </div>
    <table class="list"><thead><tr><th>When</th><th>Mistake</th><th>Type</th><th>Why</th><th>App</th></tr></thead><tbody>
    ${
      rows
        .map(
          (r) => `<tr>
        <td class="small muted nowrap">${formatTime(r.createdAt)}</td>
        <td><div class="diff"><del>${esc(r.original)}</del> → <ins>${esc(r.corrected)}</ins></div>
          <div class="small muted">${esc(r.sentence)}</div></td>
        <td><span class="badge ${r.severity}">${r.severity === "error" ? "Error" : "Suggestion"}</span><div class="small">${esc(categoryLabel(r.category, lang))}</div>
          <div class="small muted">${esc(r.pattern)}</div></td>
        <td class="small">${esc((lang === "en" ? r.explanationEn : r.explanationZh) || r.explanationEn || r.explanationZh)}</td>
        <td class="small muted">${esc(r.sourceAppName || r.sourceApp || "Clipboard")}</td>
      </tr>`,
        )
        .join("") || `<tr><td colspan="5" class="muted">No mistakes match.</td></tr>`
    }</tbody></table>`;
  const save = (patch: Partial<Filter>) => {
    sessionStorage.setItem("filter", JSON.stringify({ ...f, ...patch }));
    void show("mistakes");
  };
  $<HTMLSelectElement>("#f-category").onchange = (e) => save({ category: (e.target as HTMLSelectElement).value });
  $<HTMLSelectElement>("#f-severity").onchange = (e) => save({ severity: (e.target as HTMLSelectElement).value });
  $<HTMLSelectElement>("#f-app").onchange = (e) => save({ sourceApp: (e.target as HTMLSelectElement).value });
  $<HTMLSelectElement>("#f-since").onchange = (e) => {
    const d = Number((e.target as HTMLSelectElement).value);
    save({ since: d ? Date.now() - d * 86400000 : 0 });
  };
  const clear = view.querySelector<HTMLElement>("#f-clear-pattern");
  if (clear) clear.onclick = (e) => (e.preventDefault(), save({ pattern: "" }));
}

// ---- Capture lab (step 0) ----

const labTargets = ["Amp (desktop app)", "Amp CLI in the terminal", "Amp web (browser)", "Octarine", "Browser text field (control)"];

async function lab(view: HTMLElement) {
  const [runs, labKey, perm] = await Promise.all([Kintsugi.labRuns(), Kintsugi.labShortcut(), Kintsugi.permission()]);
  view.innerHTML = `
    <div class="head"><h1>Capture Lab <span class="muted small">step 0</span></h1><span class="grow"></span>
      ${runs.length ? `<button id="export">Copy results as Markdown</button> <button id="clear">Clear</button>` : ""}</div>
    ${perm.available && !perm.trusted ? `<p class="error">Kintsugi doesn't have the Accessibility permission yet: grant it in Settings first.</p>` : ""}
    ${!perm.available ? `<p class="error">The capture module is not loaded: build with <code>bun run dev</code> on a Mac.</p>` : ""}
    <ol class="steps">
      <li>In each place below, type a sentence, <b>select part of it</b>, and press <kbd>${pretty(labKey)}</kbd>.
        Kintsugi reads the selection twice: with a simulated ⌘C, then with the accessibility API alone.</li>
      <li>Come back here and press <b>Write AX</b>, then (after reselecting the text in the app and pressing <kbd>${pretty(labKey)}</kbd> again) <b>Write ⌘V</b>.
        Each appends <code>[kintsugi … test]</code> to the selection in the app; answer whether it showed.</li>
      <li>Do it also with <b>nothing selected</b>, the caret in the field: the main shortcut then reads the whole field.</li>
    </ol>
    <p class="muted small">Places to test: ${labTargets.map(esc).join(" · ")}</p>
    ${runs.length ? "" : `<div class="empty">No runs yet.</div>`}
    <div class="runs">${runs
      .slice()
      .reverse()
      .map((r, i) => runCard(r, i === 0))
      .join("")}</div>`;
  view.querySelector<HTMLElement>("#clear")?.addEventListener("click", () => void Kintsugi.labDelete(0));
  view.querySelector<HTMLElement>("#export")?.addEventListener("click", async () => {
    await navigator.clipboard.writeText(labMarkdown(runs));
    $("#export").textContent = "Copied";
  });
  view.querySelectorAll<HTMLElement>(".run").forEach((card) => {
    const id = Number(card.dataset.id);
    const run = runs.find((r) => r.id === id)!;
    const note = card.querySelector<HTMLSelectElement>(".note")!;
    const update = () => {
      const confirmed: Record<string, string> = {};
      card.querySelectorAll<HTMLSelectElement>(".confirm").forEach((s) => s.value && (confirmed[s.dataset.m!] = s.value));
      void Kintsugi.labUpdate(id, note.value, confirmed);
    };
    note.onchange = update;
    card.querySelectorAll<HTMLSelectElement>(".confirm").forEach((s) => (s.onchange = update));
    card.querySelectorAll<HTMLButtonElement>("[data-write]").forEach((b) => {
      b.onclick = async () => {
        b.disabled = true;
        try {
          await Kintsugi.labWrite(id, b.dataset.write!);
        } catch (err) {
          alert(errorText(err));
          b.disabled = false;
        }
      };
    });
    card.querySelector<HTMLElement>(".del")!.onclick = () => void Kintsugi.labDelete(id);
    void run;
  });
}

function verdict(ok: boolean | undefined) {
  return ok === undefined ? `<span class="muted">—</span>` : ok ? `<span class="ok">✓</span>` : `<span class="bad">✗</span>`;
}

function runCard(r: LabRun, newest: boolean): string {
  const read = (m: string) => r.reads?.find((a) => a.method === m);
  const write = (m: string) => r.writes?.filter((a) => a.method === m).at(-1);
  const row = (label: string, a: { ok: boolean; detail: string; log: string[] } | undefined, extra = "") => `
    <tr><td>${label}</td><td>${verdict(a?.ok)}</td><td class="small">${esc(a?.detail ?? "not tried")}${extra}
      ${a?.log?.length ? `<details><summary class="small muted">log</summary><pre>${esc(a.log.join("\n"))}</pre></details>` : ""}</td></tr>`;
  const confirm = (m: string) =>
    `<select class="confirm" data-m="${m}"><option value="">Showed in the app?</option><option value="yes" ${r.confirmed?.[m] === "yes" ? "selected" : ""}>Yes, it showed</option><option value="no" ${r.confirmed?.[m] === "no" ? "selected" : ""}>No</option></select>`;
  return `<section class="card run" data-id="${r.id}">
    <div class="head"><h2>${esc(r.app || "?")} <span class="muted small">${esc(r.bundleId)} · ${esc(r.role || "no AX role")} · ${formatTime(r.time)}</span></h2>
      <span class="grow"></span><a href="#" class="del muted small">delete</a></div>
    <label class="small">What was this? <select class="note"><option value="">—</option>${labTargets
      .map((t) => `<option ${t === r.note ? "selected" : ""}>${esc(t)}</option>`)
      .join("")}</select></label>
    ${r.text ? `<blockquote>${esc(r.text)}</blockquote>` : ""}
    <table class="lab"><tbody>
      ${row("Read: accessibility API", read("ax"))}
      ${row("Read: ⌘C clipboard", read("clipboard"))}
      ${row("Write: accessibility API", write("ax_write"), write("ax_write") ? " " + confirm("ax") : "")}
      ${row("Write: ⌘V paste", write("paste_write"), write("paste_write") ? " " + confirm("paste") : "")}
    </tbody></table>
    ${newest ? `<div class="row"><button data-write="ax">Write AX</button><button data-write="paste">Write ⌘V</button>
      <span class="muted small">Writes into ${esc(r.app)} where the last read was.</span></div>` : ""}
  </section>`;
}

function labMarkdown(runs: LabRun[]): string {
  const y = (ok?: boolean) => (ok === undefined ? "—" : ok ? "✓" : "✗");
  const lines = ["| Place | App (bundle ID) | AX role | AX read | ⌘C read | AX write | ⌘V write |", "|---|---|---|---|---|---|---|"];
  for (const r of runs) {
    const w = (m: string) => {
      const a = r.writes?.filter((x) => x.method === m + "_write").at(-1);
      if (!a) return "—";
      const c = r.confirmed?.[m];
      return a.ok && c ? (c === "yes" ? "✓" : "✗ (posted, not shown)") : y(a.ok) + (a.ok ? " (unconfirmed)" : "");
    };
    lines.push(
      `| ${r.note || "?"} | ${r.app} (${r.bundleId}) | ${r.role || "—"} | ${y(r.reads?.find((a) => a.method === "ax")?.ok)} | ${y(r.reads?.find((a) => a.method === "clipboard")?.ok)} | ${w("ax")} | ${w("paste")} |`,
    );
  }
  return lines.join("\n") + "\n";
}

// ---- Settings ----

async function settingsView(view: HTMLElement) {
  const [hasKey, perm, login] = await Promise.all([Kintsugi.hasAPIKey(), Kintsugi.permission(), Kintsugi.openAtLogin()]);
  const s = settings;
  const modes = Object.entries(s.appModes ?? {});
  view.innerHTML = `
    <div class="head"><h1>Settings</h1></div>
    <section class="card form">
      <h2>SiliconFlow</h2>
      <label>API key <span class="muted small">${hasKey ? "saved in the Keychain" : "not set"}</span>
        <div class="row"><input id="key" type="password" placeholder="${hasKey ? "•••••••• (leave empty to keep)" : "sk-…"}" autocomplete="off" />
        <button id="save-key">Save key</button>${hasKey ? `<button id="del-key">Remove</button>` : ""}</div></label>
      <label>API address <input id="baseURL" value="${esc(s.baseURL)}" />
        <span class="muted small">https://api.siliconflow.cn/v1 (China) or https://api.siliconflow.com/v1 (global): the key works only with its own.</span></label>
      <label>Checking model <input id="checkModel" value="${esc(s.checkModel)}" list="models" /></label>
      <label>High-quality model <span class="muted small">for weekly summaries and drills (later)</span><input id="qualityModel" value="${esc(s.qualityModel)}" list="models" /></label>
      <datalist id="models"><option>deepseek-ai/DeepSeek-V4.1-Flash</option><option>zai-org/GLM-5.3-Flash</option><option>deepseek-ai/DeepSeek-V4-Pro</option><option>moonshotai/Kimi-K3</option></datalist>
      <div class="row"><button id="test">Test the checking model</button><span id="test-out" class="small muted"></span></div>
    </section>
    <section class="card form">
      <h2>Checking</h2>
      <label>Explanations in <select id="language"><option value="zh" ${s.language === "zh" ? "selected" : ""}>中文</option><option value="en" ${s.language === "en" ? "selected" : ""}>English</option></select></label>
      <label>Shortcut <input id="shortcut" value="${esc(s.shortcut)}" /> <span class="muted small">e.g. Alt+Cmd+G (⌥⌘G), Ctrl+Shift+K</span></label>
      <label class="check"><input id="login" type="checkbox" ${login ? "checked" : ""} /> Open at login</label>
    </section>
    <section class="card form">
      <h2>Accessibility</h2>
      ${
        !perm.available
          ? `<p class="muted">The capture module is not loaded here: Kintsugi reads only what you copy.</p>`
          : perm.trusted
            ? `<p class="ok">✓ Kintsugi may read and write the selection of other apps.</p>`
            : `<p>Kintsugi needs the Accessibility permission to read the selection and write corrections back.</p><button id="grant" class="primary">Grant Accessibility…</button>`
      }
      <h3>Per-app capture</h3>
      <p class="muted small">Auto: accessibility API, then ⌘C/⌘V. Clipboard: ⌘C/⌘V only. Manual: you copy, Kintsugi reads the clipboard and puts the correction back on it.</p>
      <table class="modes"><tbody>${modes
        .map(([id, m]) => `<tr><td>${esc(id)}</td><td>${modeSelect(id, m)}</td><td><a href="#" data-del-mode="${esc(id)}">remove</a></td></tr>`)
        .join("")}
        <tr><td><input id="new-mode-id" placeholder="bundle ID, e.g. com.googlecode.iterm2" /></td><td>${modeSelect("", "clipboard", "new-mode")}</td><td><button id="add-mode">Add</button></td></tr>
      </tbody></table>
    </section>
    <div class="row"><button id="save" class="primary">Save settings</button><span id="save-out" class="small"></span></div>`;

  const val = (id: string) => $<HTMLInputElement>("#" + id).value;
  const collect = (): Settings => {
    const appModes: Record<string, CaptureMode> = { ...(s.appModes ?? {}) };
    view.querySelectorAll<HTMLSelectElement>("select[data-mode]").forEach((sel) => (appModes[sel.dataset.mode!] = sel.value as CaptureMode));
    return { ...s, baseURL: val("baseURL"), checkModel: val("checkModel"), qualityModel: val("qualityModel"), shortcut: val("shortcut"), language: val("language") as Settings["language"], appModes };
  };
  const save = async () => {
    try {
      settings = await Kintsugi.saveSettings(collect());
      $("#save-out").innerHTML = `<span class="ok">Saved</span>`;
    } catch (err) {
      $("#save-out").innerHTML = `<span class="bad">${esc(errorText(err))}</span>`;
    }
  };
  $("#save").onclick = save;
  $("#save-key").onclick = async () => {
    const k = val("key").trim();
    if (!k) return;
    await Kintsugi.setAPIKey(k);
    void show("settings");
  };
  view.querySelector<HTMLElement>("#del-key")?.addEventListener("click", async () => {
    await Kintsugi.setAPIKey("");
    void show("settings");
  });
  $("#test").onclick = async () => {
    const out = $("#test-out");
    out.textContent = "Checking…";
    try {
      await save();
      out.textContent = await Kintsugi.testModel(val("checkModel"));
    } catch (err) {
      out.innerHTML = `<span class="bad">${esc(errorText(err))}</span>`;
    }
  };
  $<HTMLInputElement>("#login").onchange = async (e) => {
    try {
      await Kintsugi.setOpenAtLogin((e.target as HTMLInputElement).checked);
    } catch (err) {
      alert(errorText(err));
    }
  };
  view.querySelector<HTMLElement>("#grant")?.addEventListener("click", async () => {
    await Kintsugi.guidePermission().catch(() => {});
    void show("settings");
  });
  view.querySelectorAll<HTMLElement>("[data-del-mode]").forEach((a) => {
    a.onclick = async (e) => {
      e.preventDefault();
      const next = collect();
      delete next.appModes[a.dataset.delMode!];
      settings = await Kintsugi.saveSettings(next);
      void show("settings");
    };
  });
  $("#add-mode").onclick = async () => {
    const id = val("new-mode-id").trim();
    if (!id) return;
    const next = collect();
    next.appModes[id] = $<HTMLSelectElement>("#new-mode").value as CaptureMode;
    try {
      settings = await Kintsugi.saveSettings(next);
      void show("settings");
    } catch (err) {
      $("#save-out").innerHTML = `<span class="bad">${esc(errorText(err))}</span>`;
    }
  };
}

function modeSelect(id: string, m: string, elId = "") {
  return `<select ${elId ? `id="${elId}"` : `data-mode="${esc(id)}"`}>${["clipboard", "manual"].map((x) => `<option ${x === m ? "selected" : ""}>${x}</option>`).join("")}</select>`;
}

// ---- First run ----

async function welcome(view: HTMLElement) {
  const [hasKey, perm] = await Promise.all([Kintsugi.hasAPIKey(), Kintsugi.permission()]);
  view.innerHTML = `
    <div class="welcome">
      <h1><span class="crack"></span>Welcome to Kintsugi</h1>
      <p class="muted">Mistakes aren't erased — they're kept, and mended with gold.</p>
      <section class="card">
        <h2>1. Accessibility ${perm.trusted ? `<span class="ok">✓</span>` : ""}</h2>
        <p>To read the text you select in Amp, Octarine or a browser and write corrections back, Kintsugi needs the Accessibility permission.</p>
        ${perm.available && !perm.trusted ? `<button id="grant" class="primary">Open System Settings…</button>` : perm.available ? "" : `<p class="muted">Not available in this build: Kintsugi will read what you copy.</p>`}
      </section>
      <section class="card">
        <h2>2. SiliconFlow API key ${hasKey ? `<span class="ok">✓</span>` : ""}</h2>
        <p>Kept in your Keychain. Get one at <code>cloud.siliconflow.cn</code>.</p>
        <div class="row"><input id="key" type="password" placeholder="${hasKey ? "saved" : "sk-…"}" /><button id="save-key">Save</button></div>
        <span id="key-out" class="small"></span>
      </section>
      <section class="card">
        <h2>3. Try it</h2>
        <p>Select some English you wrote anywhere and press <kbd>${pretty(settings.shortcut)}</kbd>. With nothing selected, Kintsugi checks the whole field.</p>
      </section>
      <div class="row"><button id="done" class="primary">${hasKey && (perm.trusted || !perm.available) ? "Start" : "Skip for now"}</button></div>
    </div>`;
  view.querySelector<HTMLElement>("#grant")?.addEventListener("click", async () => {
    await Kintsugi.guidePermission().catch(() => {});
    void show("welcome");
  });
  $("#save-key").onclick = async () => {
    const k = $<HTMLInputElement>("#key").value.trim();
    if (!k) return;
    try {
      await Kintsugi.setAPIKey(k);
      void show("welcome");
    } catch (err) {
      $("#key-out").innerHTML = `<span class="bad">${esc(errorText(err))}</span>`;
    }
  };
  $("#done").onclick = async () => {
    await Kintsugi.finishOnboarding();
    void show("overview");
  };
}
