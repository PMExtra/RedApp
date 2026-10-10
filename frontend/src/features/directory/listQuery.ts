import { computed, ref, watch } from "vue";
import { useRoute, useRouter, type LocationQueryRaw } from "vue-router";
import { useDebounced } from "@/shared/lib";
import type { DirectoryState } from "./queries";

const STATES: readonly DirectoryState[] = ["current", "enabled", "disabled", "deleted"];
const SEARCH_DELAY_MS = 250;

/**
 * Search text, lifecycle filter and page of a list, kept in the URL query
 * (`?q=&state=&page=`) so views can be shared and survive reloads. Typing is
 * debounced; changing the search or filter goes back to page 1. `deleted` is
 * never offered by the filter buttons but honoured when the URL asks for it.
 */
export function useListQuery() {
  const route = useRoute();
  const router = useRouter();

  const q = computed(() => (typeof route.query.q === "string" ? route.query.q.trim() : ""));
  const state = computed<DirectoryState>(() => {
    const value = route.query.state;
    return STATES.find((item) => item === value) ?? "current";
  });
  const page = computed(() => {
    const value = Number(route.query.page);
    return Number.isSafeInteger(value) && value >= 1 ? value : 1;
  });

  function update(next: { q?: string; state?: DirectoryState; page?: number }): void {
    const query: LocationQueryRaw = { ...route.query };
    const resetPage = (next.q !== undefined && next.q !== q.value) || next.state !== undefined;
    if (next.q !== undefined) query.q = next.q || undefined;
    if (next.state !== undefined) query.state = next.state === "current" ? undefined : next.state;
    const targetPage = next.page ?? (resetPage ? 1 : page.value);
    query.page = targetPage > 1 ? String(targetPage) : undefined;
    void router.replace({ query });
  }

  /** The text in the search box; written to the URL after a short pause. */
  const search = ref(q.value);
  const debounced = useDebounced(search, SEARCH_DELAY_MS);
  watch(debounced, (value) => {
    if (value.trim() !== q.value) update({ q: value.trim() });
  });
  watch(q, (value) => {
    if (value !== search.value.trim()) search.value = value;
  });

  return {
    q,
    state,
    page,
    search,
    setState: (value: DirectoryState) => {
      update({ state: value });
    },
    setPage: (value: number) => {
      update({ page: value });
    },
    /** `true` when the list is narrowed by search or filter. */
    filtered: computed(() => q.value !== "" || state.value !== "current"),
  };
}
