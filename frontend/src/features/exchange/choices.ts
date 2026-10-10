import type { ImportChoice, ImportItem } from "./queries";

export type ProxyResolution = "keep_current" | "keep_effective" | "inherit" | "direct" | "url";

/** The user's decisions for one package item, before serialization. */
export interface ChoiceDraft {
  action?: "create" | "update" | "skip";
  target_vendor?: string;
  target_id?: string;
  dictionary_update?: boolean;
  proxy?: ProxyResolution;
  proxy_url?: string;
  detach_template?: boolean;
  update_notes?: boolean;
}

export function itemKey(item: Pick<ImportItem, "kind" | "key">): string {
  return `${item.kind}:${item.key}`;
}

/** The item asks how to resolve its proxy (omitted credentials or changed inheritance). */
export function needsProxy(item: ImportItem): boolean {
  return (
    item.omitted_fields.includes("proxy") ||
    item.requirements.includes("resolve_omitted_proxy") ||
    item.requirements.includes("resolve_changed_proxy")
  );
}

export function offersDetach(item: ImportItem): boolean {
  return item.detach_template || item.requirements.includes("confirm_detach_template");
}

export function offersNotes(item: ImportItem): boolean {
  return (
    item.requirements.includes("confirm_notes_update") ||
    item.differences.some((difference) => difference.field === "admin_notes")
  );
}

/** `ImportChoice` objects for the next preview; only explicit decisions are sent. */
export function serializeChoices(
  items: readonly ImportItem[],
  drafts: Readonly<Record<string, ChoiceDraft>>,
): ImportChoice[] {
  const choices: ImportChoice[] = [];
  for (const item of items) {
    const draft = drafts[itemKey(item)];
    if (!draft) continue;
    const choice: ImportChoice = { kind: item.kind, key: item.key };
    if (draft.action) choice.action = draft.action;
    if (draft.target_vendor) choice.target_vendor = draft.target_vendor;
    if (draft.target_id) choice.target_id = draft.target_id;
    if (draft.dictionary_update) choice.dictionary_update = true;
    if (draft.detach_template) choice.detach_template = true;
    if (draft.update_notes) choice.update_notes = true;
    if (draft.proxy === "keep_effective") choice.keep_effective_proxy = true;
    else if (draft.proxy === "url") choice.proxy = { mode: "url", url: draft.proxy_url ?? "" };
    else if (draft.proxy === "inherit" || draft.proxy === "direct") {
      choice.proxy = { mode: draft.proxy };
    }
    if (Object.keys(choice).length > 2) choices.push(choice);
  }
  return choices;
}

/** Readable text of a difference value (strings verbatim, anything else as JSON). */
export function differenceText(value: unknown): string {
  if (value === null || value === undefined) return "";
  return typeof value === "string" ? value : JSON.stringify(value, null, 2);
}
