import { computed, onUnmounted, ref, watch, type Ref } from "vue";
import { api } from "../api";
import { appAPI } from "../bootstrap";

export interface SourceEpoch {
  epoch: number;
  base_url: string;
  base_urls?: string[];
  source_strategy?: "ordered" | "round_robin" | "random";
  current: boolean;
  active: boolean;
  created_at: string;
}
export function useSourceEpoch(application: Ref<string>) {
  const sources = ref<SourceEpoch[]>([]), selected = ref("");
  const loading = ref(false), error = ref<unknown>();
  const query = computed(() => selected.value ? `?source_epoch=${encodeURIComponent(selected.value)}` : "");
  let ticket = 0, controller: AbortController | undefined;
  async function load() {
    controller?.abort();
    const request = new AbortController(), attempt = ++ticket;
    controller = request;
    loading.value = true;
    error.value = undefined;
    try {
      const response = await api<{ sources: SourceEpoch[] }>(`${appAPI(application.value)}/sources`, undefined, request.signal);
      if (attempt !== ticket) return;
      if (!Array.isArray(response.sources)) throw Error("Sources unavailable");
      sources.value = response.sources;
      if (selected.value && !sources.value.some((source) => String(source.epoch) === selected.value && !source.current)) selected.value = "";
    } catch (reason) {
      if (attempt === ticket) error.value = reason;
    } finally {
      if (attempt === ticket) { controller = undefined; loading.value = false; }
    }
  }
  watch(application, () => {
    selected.value = "";
    sources.value = [];
    void load();
  }, { immediate: true, flush: "sync" });
  onUnmounted(() => { ticket++; controller?.abort(); });
  return { sources, selected, query, loading, error, load };
}
