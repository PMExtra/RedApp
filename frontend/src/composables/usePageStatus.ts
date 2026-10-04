import { onMounted, onUnmounted, ref, shallowRef, watch, type Ref } from "vue";
import { api, type Status } from "../api";
import { signedIn } from "../session";
export function usePageStatus<T = Status>(path: Ref<string>, enabled: Ref<boolean> = ref(true)) {
  const status = shallowRef<T>(),
    error = ref<unknown>(),
    loading = ref(false),
    automatic = ref(true);
  let controller: AbortController | undefined,
    timer: ReturnType<typeof setTimeout> | undefined,
    ticket = 0,
    disposed = false;
  function stop() {
    ticket++;
    clearTimeout(timer);
    controller?.abort();
    controller = undefined;
    loading.value = false;
  }
  function schedule() {
    clearTimeout(timer);
    if (
      !disposed &&
      enabled.value &&
      signedIn.value &&
      automatic.value &&
      document.visibilityState !== "hidden"
    )
      timer = setTimeout(() => void refresh(), 5000);
  }
  async function refresh() {
    if (
      disposed ||
      !enabled.value ||
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
      const data = await api<T>(path.value, undefined, request.signal);
      if (attempt === ticket) {
        status.value = data;
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
  const visible = () => {
    if (document.visibilityState === "hidden") stop();
    else void refresh();
  };
  watch(path, () => {
    stop();
    status.value = undefined;
    error.value = undefined;
    void refresh();
  });
  watch(enabled, (value) => {
    stop();
    if (value) void refresh();
  }, { flush: "sync" });
  watch(signedIn, (value) => {
    if (!value) {
      stop();
      status.value = undefined;
    } else void refresh();
  });
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
  return { status, error, loading, automatic, refresh };
}
