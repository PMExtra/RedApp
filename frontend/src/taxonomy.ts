import { api } from "./api";
import type { Configuration } from "./configuration";
import type { LocalizedText } from "./site";
export type CategoryOption = { id: string; name: LocalizedText };
export type CategoryItem = Omit<Configuration, "proxy_effective"> &
  CategoryOption & { kind: "categories"; builtin: boolean; applications: number };
// Mirrors the server's literal comparison: NFC and case-insensitive.
export function foldText(value: string) {
  return value.normalize("NFC").toLocaleLowerCase();
}
export async function allCategories(signal?: AbortSignal): Promise<CategoryItem[]> {
  const items: CategoryItem[] = [];
  for (let page = 1; ; page++) {
    const result = await api<{ items: CategoryItem[]; total_pages: number }>(
      `categories?page=${page}&limit=100`,
      undefined,
      signal,
    );
    if (!Array.isArray(result.items)) throw new Error("Categories unavailable");
    items.push(...result.items);
    if (page >= result.total_pages) return items;
  }
}
