import { computed, ref, watch, type Ref } from "vue";
import { copy, same } from "./overlay";
import type { AppConfiguration, AppConfigurationPatch } from "./queries";

type Path = NonNullable<AppConfigurationPatch["unset"]>[number];

/**
 * Draft state for a group of configuration paths: which paths are restored
 * to the template (`unset`), which differ from the saved value (`set`), and
 * whether anything is unsaved. `read(spec)` maps an `AppSpec` to the draft
 * value of each path; the caller owns the draft values.
 *
 * For editors that are not field forms (rule lists, retention and prewarm
 * policies) with hand-written validation; field forms use `useOverlayForm`.
 * The caller installs `useDirtyGuard(draft.dirty)`.
 */
export function useOverlayDraft<Values extends Partial<Record<Path, unknown>>>(
  configuration: Ref<AppConfiguration | undefined>,
  read: (spec: AppConfiguration["effective"] | null) => Values | undefined,
) {
  const draft: Ref<Values | undefined> = ref();
  /** The saved values the draft started from. */
  const baseline: Ref<Values | undefined> = ref();
  const resets = ref(new Set<Path>());
  const saved = computed(() => read(configuration.value?.effective ?? null));
  const template = computed(() => read(configuration.value?.defaults ?? null));
  const paths = computed(() => Object.keys(baseline.value ?? {}) as (keyof Values & Path)[]);

  function modified(path: keyof Values & Path): boolean {
    return !!draft.value && !!baseline.value && !same(draft.value[path], baseline.value[path]);
  }

  /** The PATCH body, or `null` when nothing changed. */
  const patch = computed<AppConfigurationPatch | null>(() => {
    const current = draft.value;
    if (!current) return null;
    const set: Record<string, unknown> = {};
    const unset: Path[] = [];
    for (const path of paths.value) {
      const restored =
        resets.value.has(path) && template.value && same(current[path], template.value[path]);
      if (restored) unset.push(path);
      else if (modified(path)) set[path] = current[path];
    }
    if (unset.length === 0 && Object.keys(set).length === 0) return null;
    return {
      ...(Object.keys(set).length ? { set } : {}),
      ...(unset.length ? { unset } : {}),
    };
  });
  const dirty = computed(() => patch.value !== null);

  function reset() {
    baseline.value = saved.value ? copy(saved.value) : undefined;
    draft.value = saved.value ? copy(saved.value) : undefined;
    resets.value = new Set();
  }

  /** Restores the template value of one path (FieldReset). */
  function restore(path: keyof Values & Path) {
    if (!draft.value || !template.value) return;
    draft.value = { ...draft.value, [path]: copy(template.value[path]) };
    if (configuration.value?.fields[path]?.source === "custom") {
      resets.value = new Set([...resets.value, path]);
    }
  }

  // Follow the saved state (other sections' saves, refetches) unless editing.
  watch(
    saved,
    () => {
      if (!dirty.value) reset();
    },
    { immediate: true },
  );

  return { draft, saved, template, dirty, patch, modified, reset, restore };
}
