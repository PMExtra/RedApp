import { computed, onUnmounted, ref, watch, type Ref } from "vue";
import { api, isCancellation } from "../api";
import { useDirtyDraft } from "./useDirtyDraft";
import { getLeaf, setLeaf, type Configuration } from "../configuration";

// Projection selects the form's leaves; the authority remains the whole config.
export function useConfiguration<T extends object>(
  path: Ref<string>,
  prefix: string,
  leaves: string[],
  applied?: () => void,
) {
  const draft = ref<T>() as Ref<T | undefined>,
    configuration = ref<Configuration>(),
    baseline = ref("");
  const touched = ref(new Set<string>()),
    unsets = ref(new Set<string>());
  const loading = ref(false),
    saving = ref(false),
    saved = ref(false),
    error = ref<unknown>();
  const dirty = computed(
    () =>
      !!draft.value &&
      (JSON.stringify(draft.value) !== baseline.value ||
        touched.value.size > 0 ||
        unsets.value.size > 0),
  );
  const confirmDiscard = useDirtyDraft(dirty);
  let ticket = 0,
    controller: AbortController | undefined;
  function accept(value: Configuration) {
    configuration.value = value;
    draft.value = structuredClone(
      prefix ? getLeaf(value.effective, prefix) : value.effective,
    ) as T;
    baseline.value = JSON.stringify(draft.value);
    touched.value = new Set();
    unsets.value = new Set();
  }
  function mark(leaf: string) {
    touched.value.add(leaf);
    unsets.value.delete(leaf);
    saved.value = false;
  }
  const original = computed(() =>
    baseline.value ? (JSON.parse(baseline.value) as T) : undefined,
  );
  function modified(leaf: string) {
    return (
      touched.value.has(leaf) ||
      (!!draft.value &&
        JSON.stringify(getLeaf(draft.value, leaf)) !==
          JSON.stringify(getLeaf(original.value, leaf)))
    );
  }
  function restore(leaf: string) {
    const config = configuration.value;
    if (!draft.value || !config?.template_ref || !config.defaults) return;
    const path = prefix ? `${prefix}.${leaf}` : leaf;
    setLeaf(draft.value, leaf, getLeaf(config.defaults, path));
    touched.value.delete(leaf);
    // Only a stored override needs an unset; an inherited field is simply reverted.
    if (config.fields[path]?.source === "custom") unsets.value.add(leaf);
    saved.value = false;
  }
  async function load(confirm = true) {
    if (confirm && !confirmDiscard()) return;
    controller?.abort();
    const request = new AbortController(),
      attempt = ++ticket;
    controller = request;
    loading.value = true;
    saving.value = false;
    error.value = undefined;
    saved.value = false;
    draft.value = undefined;
    configuration.value = undefined;
    touched.value = new Set();
    unsets.value = new Set();
    try {
      const value = await api<Configuration>(
        path.value,
        undefined,
        request.signal,
      );
      if (attempt === ticket) accept(value);
    } catch (reason) {
      if (attempt === ticket && !isCancellation(reason)) error.value = reason;
    } finally {
      if (attempt === ticket) {
        loading.value = false;
        controller = undefined;
      }
    }
  }
  // Templates bind save directly to submit events, so request-only fields use saveWith.
  function save() {
    return saveWith({});
  }
  // extra carries request-only fields such as new_categories alongside the field patch.
  async function saveWith(extra: Record<string, unknown>) {
    if (!draft.value || !configuration.value || loading.value || saving.value)
      return;
    const set: Record<string, unknown> = {};
    for (const leaf of leaves) {
      if (unsets.value.has(leaf)) continue;
      if (modified(leaf))
        set[prefix ? `${prefix}.${leaf}` : leaf] = getLeaf(draft.value, leaf);
    }
    const unset = [...unsets.value].map((leaf) =>
      prefix ? `${prefix}.${leaf}` : leaf,
    );
    if (!Object.keys(set).length && !unset.length) return;
    const request = new AbortController(),
      attempt = ++ticket;
    controller = request;
    saving.value = true;
    error.value = undefined;
    saved.value = false;
    try {
      const value = await api<Configuration>(
        path.value,
        { revision: configuration.value.revision, set, unset, ...extra },
        request.signal,
        {},
        "PATCH",
      );
      if (attempt === ticket) {
        accept(value);
        saved.value = true;
        applied?.();
      }
    } catch (reason) {
      if (attempt === ticket && !isCancellation(reason)) error.value = reason;
    } finally {
      if (attempt === ticket) {
        saving.value = false;
        controller = undefined;
      }
    }
  }
  watch(path, () => void load(false), { immediate: true, flush: "sync" });
  onUnmounted(() => {
    ticket++;
    controller?.abort();
  });
  return {
    draft,
    configuration,
    touched,
    unsets,
    loading,
    saving,
    saved,
    error,
    dirty,
    load,
    save,
    saveWith,
    mark,
    restore,
    modified,
  };
}
