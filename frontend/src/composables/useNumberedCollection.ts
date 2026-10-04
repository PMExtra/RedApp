import {
  computed,
  onMounted,
  onUnmounted,
  ref,
  shallowRef,
  watch,
  type Ref,
} from "vue";
import { api } from "../api";
import { signedIn } from "../session";
export interface NumberedPage<T> {
  items: T[];
  page: number;
  limit: number;
  total: number;
  total_pages: number;
}
// Only the current page is retained. Search, app and filter changes cancel the
// previous request; a late response cannot alter the new page or its errors.
export function useNumberedCollection<T>(
  path: Ref<string>,
  pageSize = 25,
  automatic: Ref<boolean> = ref(false),
) {
  const items = shallowRef<T[]>([]),
    page = ref(1),
    total = ref(0),
    totalPages = ref(1),
    loaded = ref(false),
    loading = ref(false),
    error = shallowRef<unknown>();
  let controller: AbortController | undefined,
    timer: ReturnType<typeof setTimeout> | undefined,
    ticket = 0,
    disposed = false;
  function stop() {
    ticket++;
    controller?.abort();
    controller = undefined;
    clearTimeout(timer);
    loading.value = false;
  }
  function schedule() {
    clearTimeout(timer);
    if (
      !disposed &&
      automatic.value &&
      signedIn.value &&
      document.visibilityState !== "hidden"
    )
      timer = setTimeout(() => void refresh(), 5000);
  }
  async function refresh() {
    if (
      disposed ||
      controller ||
      !signedIn.value ||
      document.visibilityState === "hidden"
    )
      return;
    clearTimeout(timer);
    const attempt = ++ticket,
      request = new AbortController();
    controller = request;
    loading.value = true;
    try {
      const query = new URLSearchParams({
        page: String(page.value),
        limit: String(pageSize),
      });
      const data = await api<NumberedPage<T>>(
        `${path.value}${path.value.includes("?") ? "&" : "?"}${query}`,
        undefined,
        request.signal,
      );
      if (attempt !== ticket) return;
      if (
        !Array.isArray(data.items) ||
        !Number.isInteger(data.page) ||
        data.page < 1 ||
        !Number.isInteger(data.total_pages) ||
        data.total_pages < 1 ||
        data.page > data.total_pages ||
        !Number.isSafeInteger(data.total) ||
        data.total < 0
      )
        throw Error("Invalid page response");
      items.value = data.items;
      page.value = data.page;
      total.value = data.total;
      totalPages.value = data.total_pages;
      loaded.value = true;
      error.value = undefined;
    } catch (reason) {
      if (attempt === ticket) error.value = reason;
    } finally {
      if (attempt === ticket) {
        controller = undefined;
        loading.value = false;
        schedule();
      }
    }
  }
  function go(value: number) {
    if (
      loading.value ||
      !Number.isInteger(value) ||
      value < 1 ||
      value > totalPages.value
    )
      return;
    stop();
    page.value = value;
    items.value = [];
    loaded.value = false;
    error.value = undefined;
    void refresh();
  }
  function reset() {
    stop();
    items.value = [];
    page.value = 1;
    total.value = 0;
    totalPages.value = 1;
    loaded.value = false;
    error.value = undefined;
  }
  watch(
    path,
    () => {
      reset();
      void refresh();
    },
    { flush: "sync" },
  );
  watch(
    signedIn,
    (value) => {
      reset();
      if (value) void refresh();
    },
    { flush: "sync" },
  );
  watch(automatic, (value) => {
    if (value) schedule();
    else clearTimeout(timer);
  });
  const visible = () => {
    if (document.visibilityState === "hidden") stop();
    else void refresh();
  };
  onMounted(() => {
    void refresh();
    document.addEventListener("visibilitychange", visible);
  });
  onUnmounted(() => {
    disposed = true;
    stop();
    document.removeEventListener("visibilitychange", visible);
  });
  return {
    items,
    page,
    total,
    totalPages,
    loaded,
    loading,
    error,
    refresh,
    go,
    previous: () => go(page.value - 1),
    next: () => go(page.value + 1),
    previousAvailable: computed(() => page.value > 1),
    nextAvailable: computed(() => page.value < totalPages.value),
  };
}
