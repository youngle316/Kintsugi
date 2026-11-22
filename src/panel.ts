// The correction panel: shown by the shortcut over the app the user writes
// in, which stays in front. Replace writes back, Copy copies, Esc ignores.
import { Kintsugi, events, type Check, type Issue } from "./mygo";
import { $, categoryLabel, esc, errorText } from "./util";

let current: Check | null = null;
let busy = false;

export async function start() {
  document.body.innerHTML = `<div id="panel" class="panel"></div>`;
  events.checkChanged.on((c) => render(c));
  const c = await Kintsugi.current();
  if (c) render(c);
  addEventListener("keydown", onKey);
}

function onKey(e: KeyboardEvent) {
  if (e.key === "Escape") {
    e.preventDefault();
    void Kintsugi.dismiss();
  } else if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && current?.state === "done") {
    e.preventDefault();
    void act("replace");
  } else if (e.key === "c" && e.metaKey && !getSelection()?.toString() && current?.state === "done") {
    e.preventDefault();
    void act("copy");
  }
}

async function act(action: "replace" | "copy" | "ignore") {
  if (!current || busy) return;
  const c = current;
  const text = c.result?.correctedText ?? c.original;
  busy = true;
  try {
    if (action === "replace") await Kintsugi.replace(c.id, text);
    else if (action === "copy") await Kintsugi.copy(c.id, text);
    else await Kintsugi.dismiss();
  } catch (err) {
    // The panel is hidden by now: the error shows the next time.
    console.error(errorText(err));
  } finally {
    busy = false;
  }
}

// highlight marks each issue's span in the corrected text, the first time
// it appears after the previous one.
function highlight(text: string, issues: Issue[]): string {
  let out = "";
  let at = 0;
  issues.forEach((is, i) => {
    const span = is.corrected;
    if (!span) return;
    const j = text.indexOf(span, at);
    if (j < 0) return;
    out += esc(text.slice(at, j));
    out += `<mark class="${is.severity}" data-i="${i}">${esc(span)}</mark>`;
    at = j + span.length;
  });
  return out + esc(text.slice(at));
}

function render(c: Check) {
  current = c;
  const root = $("#panel");
  const source = c.sourceName ? `<span class="muted">${esc(c.sourceName)}</span>` : "";
  if (c.state === "loading") {
    root.innerHTML = `
      <header><strong>Kintsugi</strong>${source}</header>
      <div class="loading"><span class="spinner"></span> Checking ${c.original ? `${c.original.split(/\s+/).length} words` : ""}…</div>
      <footer><span class="muted">Esc to close</span></footer>`;
    return;
  }
  if (c.state === "error") {
    root.innerHTML = `
      <header><strong>Kintsugi</strong>${source}</header>
      <p class="error">${esc(c.error)}</p>
      <footer><span class="muted">Esc to close</span><span class="grow"></span>
        <button id="settings">Settings…</button></footer>`;
    $("#settings").onclick = () => {
      void Kintsugi.openWindow("settings");
      void Kintsugi.dismiss();
    };
    return;
  }
  const res = c.result!;
  const lang = c.language;
  const errors = res.issues.filter((i) => i.severity === "error").length;
  const suggestions = res.issues.length - errors;
  const body = res.issues.length
    ? `<div class="corrected">${highlight(res.correctedText, res.issues)}</div>
       <ol class="issues">${res.issues
         .map(
           (is, i) => `<li data-i="${i}">
             <div><span class="badge ${is.severity}">${is.severity === "error" ? "Error" : "Suggestion"}</span>
               <span class="cat">${esc(categoryLabel(is.category, lang))}</span></div>
             <div class="diff"><del>${esc(is.original)}</del> → <ins>${esc(is.corrected)}</ins></div>
             <div class="why">${esc((lang === "en" ? is.explanationEn : is.explanationZh) || is.explanationEn || is.explanationZh)}</div>
           </li>`,
         )
         .join("")}</ol>`
    : `<div class="good">✓ Looks good</div>`;
  const replaceLabel = c.manualMode || !c.canReplace ? "Copy" : c.wholeField ? "Replace all" : "Replace";
  root.innerHTML = `
    <header><strong>Kintsugi</strong>${source}<span class="grow"></span>
      <span class="muted">${errors} error${errors === 1 ? "" : "s"} · ${suggestions} suggestion${suggestions === 1 ? "" : "s"}</span></header>
    <div class="scroll">${body}</div>
    <footer>
      <span class="muted">Esc ignore · ⌘↩ ${replaceLabel.toLowerCase()}</span><span class="grow"></span>
      <button id="ignore">Ignore</button>
      ${res.issues.length ? `<button id="copy">Copy</button><button id="replace" class="primary">${replaceLabel}</button>` : `<button id="ok" class="primary">Done</button>`}
    </footer>`;
  $("#ignore").onclick = () => act("ignore");
  if (res.issues.length) {
    $("#copy").onclick = () => act("copy");
    $("#replace").onclick = () => act(c.manualMode || !c.canReplace ? "copy" : "replace");
  } else {
    $("#ok").onclick = () => act("ignore");
  }
  // Pointing at an issue lights its span.
  root.querySelectorAll<HTMLElement>(".issues li").forEach((li) => {
    li.onmouseenter = () => root.querySelector(`mark[data-i="${li.dataset.i}"]`)?.classList.add("hot");
    li.onmouseleave = () => root.querySelector(`mark[data-i="${li.dataset.i}"]`)?.classList.remove("hot");
  });
}
