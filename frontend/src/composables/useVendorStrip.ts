import { onUnmounted, ref, shallowRef, watch, type Ref } from "vue";
import { api, ApiError, isCancellation } from "../api";
import type { ManagedApplication } from "../directory";
import type { NumberedPage } from "./useNumberedCollection";

// Seed from the directory preview, then collect bounded API pages into one strip.
// No per-card paging UI; filter/vendor changes discard the previous request series.
export function useVendorStrip(source: Ref<{ id: string; query: string; state: string; apps: ManagedApplication[]; total: number }>) {
  const items = shallowRef<ManagedApplication[]>([]), total = ref(0), loading = ref(false), error = shallowRef<unknown>();
  let ticket = 0, controller: AbortController | undefined;
  function stop() { ticket++; controller?.abort(); controller = undefined; loading.value = false; }
  async function refresh() {
    stop();
    const attempt = ticket, current = source.value, request = new AbortController();
    controller = request; loading.value = true; error.value = undefined;
    const collected = new Map<string, ManagedApplication>();
    try {
      let pages = 1;
      for (let page = 1; page <= pages; page++) {
        const query = new URLSearchParams({ q: current.query, state: current.state, page: String(page), limit: "100" });
        const data = await api<NumberedPage<ManagedApplication>>(`vendors/${current.id}/apps?${query}`, undefined, request.signal);
        if (attempt !== ticket) return;
        if (!Array.isArray(data.items) || data.page !== page || !Number.isSafeInteger(data.total_pages) || data.total_pages < page || !Number.isSafeInteger(data.total) || data.total < 0) throw new ApiError({ code: "INVALID_RESPONSE" }, 200);
        if (page === 1) { pages = data.total_pages; total.value = data.total; }
        for (const app of data.items) collected.set(app.uid, app);
        items.value = [...collected.values()];
      }
    } catch (reason) {
      if (attempt === ticket && !isCancellation(reason)) error.value = reason;
    } finally {
      if (attempt === ticket) { loading.value = false; controller = undefined; }
    }
  }
  watch(source, (current) => {
    stop(); error.value = undefined; items.value = current.apps; total.value = current.total;
    if (current.total > current.apps.length) void refresh();
  }, { immediate: true, flush: "sync" });
  onUnmounted(stop);
  return { items, total, loading, error, refresh };
}
