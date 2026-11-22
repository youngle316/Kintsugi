// The main window: overview (statistics), mistakes, settings, and the
// first-run guide.
import { Chart, ArcElement, BarController, BarElement, CategoryScale, DoughnutController, Legend, LinearScale, Tooltip } from "chart.js";
import { Kintsugi, events, type Filter, type Settings } from "./mygo";
import { $, categoryLabel, esc, errorText, formatTime } from "./util";

Chart.register(ArcElement, BarController, BarElement, CategoryScale, DoughnutController, Legend, LinearScale, Tooltip);

type Tab = "overview" | "mistakes" | "settings" | "welcome";
const tabs: { id: Tab; label: string }[] = [
  { id: "overview", label: "Overview" },
  { id: "mistakes", label: "Mistakes" },
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
          </div>`
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
  const f: Filter = { category: "", severity: "", since: 0, until: 0, pattern: "", limit: 200, offset: 0, ...JSON.parse(sessionStorage.getItem("filter") ?? "{}") };
  const [rows, cats] = await Promise.all([Kintsugi.issues(f), Kintsugi.categories()]);
  const lang = settings.language;
  const sinceDays = f.since ? Math.round((Date.now() - f.since) / 86400000) : 0;
  view.innerHTML = `
    <div class="head"><h1>Mistakes</h1><span class="muted">${rows.length} shown</span></div>
    <div class="filters">
      <select id="f-category"><option value="">All types</option>${cats.map((c) => `<option value="${c}" ${c === f.category ? "selected" : ""}>${esc(categoryLabel(c, lang))}</option>`).join("")}</select>
      <select id="f-severity"><option value="">Errors & suggestions</option><option value="error" ${f.severity === "error" ? "selected" : ""}>Errors</option><option value="suggestion" ${f.severity === "suggestion" ? "selected" : ""}>Suggestions</option></select>
      <select id="f-since">${[0, 1, 7, 30, 90].map((d) => `<option value="${d}" ${d === sinceDays ? "selected" : ""}>${d ? `Last ${d} day${d > 1 ? "s" : ""}` : "Any time"}</option>`).join("")}</select>
      ${f.pattern ? `<span class="chip">pattern: ${esc(f.pattern)} <a href="#" id="f-clear-pattern">×</a></span>` : ""}
    </div>
    <table class="list"><thead><tr><th>When</th><th>Mistake</th><th>Type</th><th>Why</th></tr></thead><tbody>
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
      </tr>`,
        )
        .join("") || `<tr><td colspan="4" class="muted">No mistakes match.</td></tr>`
    }</tbody></table>`;
  const save = (patch: Partial<Filter>) => {
    sessionStorage.setItem("filter", JSON.stringify({ ...f, ...patch }));
    void show("mistakes");
  };
  $<HTMLSelectElement>("#f-category").onchange = (e) => save({ category: (e.target as HTMLSelectElement).value });
  $<HTMLSelectElement>("#f-severity").onchange = (e) => save({ severity: (e.target as HTMLSelectElement).value });
  $<HTMLSelectElement>("#f-since").onchange = (e) => {
    const d = Number((e.target as HTMLSelectElement).value);
    save({ since: d ? Date.now() - d * 86400000 : 0 });
  };
  const clear = view.querySelector<HTMLElement>("#f-clear-pattern");
  if (clear) clear.onclick = (e) => (e.preventDefault(), save({ pattern: "" }));
}

// ---- Settings ----

async function settingsView(view: HTMLElement) {
  const [hasKey, perm, login, ver] = await Promise.all([Kintsugi.hasAPIKey(), Kintsugi.permission(), Kintsugi.openAtLogin(), Kintsugi.version()]);
  const s = settings;
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
    </section>
    <section class="card form">
      <h2>About</h2>
      <p>Kintsugi ${esc(ver.version)}${ver.available ? ` · <span class="ok">${esc(ver.available)} is available</span>` : ""}</p>
      ${
        ver.canUpdate
          ? `<div class="row"><button id="updates">Check for Updates…</button><span class="muted small">Kintsugi also checks GitHub Releases once a day.</span></div>`
          : `<p class="muted small">This build doesn't update itself (development builds and builds without an update key).</p>`
      }
    </section>
    <div class="row"><button id="save" class="primary">Save settings</button><span id="save-out" class="small"></span></div>`;

  const val = (id: string) => $<HTMLInputElement>("#" + id).value;
  const collect = (): Settings => {
    return { ...s, baseURL: val("baseURL"), checkModel: val("checkModel"), qualityModel: val("qualityModel"), shortcut: val("shortcut"), language: val("language") as Settings["language"] };
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
  view.querySelector<HTMLElement>("#updates")?.addEventListener("click", () => void Kintsugi.checkForUpdates());
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
        <p>To read the text you select in any app and write corrections back, Kintsugi needs the Accessibility permission.</p>
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
