import { isCallError } from "mygo-runtime";

export const $ = <T extends HTMLElement = HTMLElement>(sel: string, root: ParentNode = document) =>
  root.querySelector<T>(sel)!;

export function esc(s: string | null | undefined): string {
  return (s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);
}

export function errorText(err: unknown): string {
  if (isCallError(err)) return err.message;
  return err instanceof Error ? err.message : String(err);
}

export const categoryLabels: Record<string, { zh: string; en: string }> = {
  article: { zh: "冠词", en: "Articles" },
  tense: { zh: "时态", en: "Tense" },
  subject_verb_agreement: { zh: "主谓一致", en: "Subject–verb agreement" },
  number_countability: { zh: "单复数与可数性", en: "Number & countability" },
  preposition_collocation: { zh: "介词与搭配", en: "Prepositions & collocations" },
  part_of_speech: { zh: "词性误用", en: "Part of speech" },
  word_choice: { zh: "词汇选择", en: "Word choice" },
  chinglish: { zh: "中式表达", en: "Chinglish" },
  sentence_structure: { zh: "句子结构", en: "Sentence structure" },
  punctuation: { zh: "标点", en: "Punctuation" },
  spelling: { zh: "拼写", en: "Spelling" },
  capitalization: { zh: "大小写", en: "Capitalization" },
  wordiness: { zh: "冗余啰嗦", en: "Wordiness" },
  tone: { zh: "语气与正式程度", en: "Tone & formality" },
};

export function categoryLabel(key: string, lang: string): string {
  const l = categoryLabels[key];
  if (!l) return key;
  return lang === "en" ? l.en : `${l.zh} · ${l.en}`;
}

export function formatTime(ms: number): string {
  return new Date(ms).toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}
