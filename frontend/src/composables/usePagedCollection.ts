import {
  computed,
  onMounted,
  onUnmounted,
  ref,
  shallowRef,
  watch,
  type Ref,
} from "vue";
import { api, type Page } from "../api";
import { signedIn } from "../session";

// A bounded current page, not an accumulated cache of every visited item.
export function usePagedCollection<T>(
  path: Ref<string>,
  automatic: Ref<boolean>,
) {
  const items = shallowRef<T[]>([]),
    loaded = ref(false),
    loading = ref(false),
    error = shallowRef<unknown>();
  const cursor = ref<string | null>(null),
    nextCursor = ref<string | null>(null),
    back = ref<(string | null)[]>([]);
  let controller: AbortController | undefined,
    timer: ReturnType<typeof setTimeout> | undefined,
    ticket = 0,
    disposed = false;
  const previousAvailable = computed(() => back.value.length > 0),
    nextAvailable = computed(() => nextCursor.value !== null);
  const page = computed(() => back.value.length + 1);
  function stop() {
    ticket++;
    clearTimeout(timer);
    controller?.abort();
    controller = undefined;
    loading.value = false;
  }
  function clearPage() {
    items.value = [];
    loaded.value = false;
    nextCursor.value = null;
    error.value = undefined;
  }
  function schedule() {
    clearTimeout(timer);
    if (
      !disposed &&
      signedIn.value &&
      automatic.value &&
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
    const query = new URLSearchParams({ limit: "50" });
    if (cursor.value !== null) query.set("cursor", cursor.value);
    try {
      const data = await api<Page<T>>(
        `${path.value}${path.value.includes("?") ? "&" : "?"}${query}`,
        undefined,
        request.signal,
      );
      if (attempt === ticket) {
        items.value = data.items;
        nextCursor.value = data.next_cursor;
        loaded.value = true;
        error.value = undefined;
      }
    } catch (reason) {
      if (
        attempt === ticket &&
        !(reason instanceof Error && reason.name === "AbortError")
      )
        error.value = reason;
    } finally {
      if (attempt === ticket) {
        controller = undefined;
        loading.value = false;
        schedule();
      }
    }
  }
  function next() {
    if (loading.value || nextCursor.value === null) return;
    back.value.push(cursor.value);
    cursor.value = nextCursor.value;
    stop();
    clearPage();
    void refresh();
  }
  function previous() {
    if (loading.value || !back.value.length) return;
    cursor.value = back.value.pop()!;
    stop();
    clearPage();
    void refresh();
  }
  function reset() {
    stop();
    cursor.value = null;
    back.value = [];
    clearPage();
  }
  const visible = () => {
    if (document.visibilityState === "hidden") stop();
    else void refresh();
  };
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
      if (!value) reset();
      else void refresh();
    },
    { flush: "sync" },
  );
  watch(automatic, (value) => {
    if (value) schedule();
    else clearTimeout(timer);
  });
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
    loaded,
    loading,
    error,
    page,
    previousAvailable,
    nextAvailable,
    refresh,
    next,
    previous,
  };
}
