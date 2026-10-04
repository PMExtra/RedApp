import { computed, onUnmounted, ref, watch, type Ref } from "vue";
import { api, isCancellation, putSetting } from "../api";
import { useDirtyDraft } from "./useDirtyDraft";
export function useSetting<T extends object>(
  path: Ref<string>,
  apply?: (value: T, saved: boolean) => void,
) {
  const draft = ref<T>() as Ref<T | undefined>,
    baseline = ref("");
  const revision = ref<number>(),
    loading = ref(false),
    saving = ref(false),
    error = ref<unknown>(),
    saved = ref(false);
  let ticket = 0,
    controller: AbortController | undefined;
  const dirty = computed(
    () => !!draft.value && JSON.stringify(draft.value) !== baseline.value,
  );
  const confirmDiscard = useDirtyDraft(dirty);
  function accept(value: T & { revision: number }, saved = false) {
    const { revision: next, ...fields } = value;
    revision.value = next;
    draft.value = structuredClone(fields) as T;
    baseline.value = JSON.stringify(fields);
    apply?.(fields as T, saved);
  }
  async function load(confirm = true) {
    if (confirm && !confirmDiscard()) return;
    controller?.abort();
    const request = new AbortController(),
      attempt = ++ticket;
    controller = request;
    draft.value = undefined;
    revision.value = undefined;
    loading.value = true;
    saving.value = false;
    error.value = undefined;
    saved.value = false;
    try {
      const value = await api<T & { revision: number }>(
        path.value,
        undefined,
        request.signal,
      );
      if (attempt === ticket) accept(value);
    } catch (reason) {
      if (attempt === ticket && !isCancellation(reason)) error.value = reason;
    } finally {
      if (attempt === ticket) {
        controller = undefined;
        loading.value = false;
      }
    }
  }
  async function save() {
    if (
      !draft.value ||
      revision.value === undefined ||
      loading.value ||
      saving.value
    )
      return;
    const request = new AbortController(),
      attempt = ++ticket;
    controller = request;
    saving.value = true;
    error.value = undefined;
    saved.value = false;
    try {
      const value = await putSetting<T & { revision: number }>(
        path.value,
        draft.value,
        revision.value,
        request.signal,
      );
      if (attempt === ticket) {
        accept(value, true);
        saved.value = true;
      }
    } catch (reason) {
      if (attempt === ticket && !isCancellation(reason)) error.value = reason;
    } finally {
      if (attempt === ticket) {
        controller = undefined;
        saving.value = false;
      }
    }
  }
  watch(path, () => void load(false), { immediate: true, flush: "sync" });
  onUnmounted(() => {
    ticket++;
    controller?.abort();
    draft.value = undefined;
  });
  return { draft, revision, loading, saving, error, saved, dirty, load, save };
}
