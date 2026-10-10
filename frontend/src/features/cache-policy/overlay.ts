import { computed, ref, watch, type Ref } from "vue";
import type { AppConfiguration, AppConfigurationPatch } from "@/features/configuration";

type Path = NonNullable<AppConfigurationPatch["unset"]>[number];

/** Structural equality of JSON values (configuration leaves). */
export function sameValue(a: unknown, b: unknown): boolean {
  return JSON.stringify(a) === JSON.stringify(b);
}

/** Deep copy of a JSON value (works on reactive proxies, unlike structuredClone). */
export function clone<T>(value: T): T {
  return value === undefined ? value : (JSON.parse(JSON.stringify(value)) as T);
}

/**
 * Draft state for a group of configuration paths: which paths are restored
 * to the template (`unset`), which differ from the saved value (`set`), and
 * whether anything is unsaved. `read(spec)` maps an `AppSpec` to the draft
 * value of each path; the caller owns the draft values.
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
    return !!draft.value && !!baseline.value && !sameValue(draft.value[path], baseline.value[path]);
  }

  /** The PATCH body, or `null` when nothing changed. */
  const patch = computed<AppConfigurationPatch | null>(() => {
    const current = draft.value;
    if (!current) return null;
    const set: Record<string, unknown> = {};
    const unset: Path[] = [];
    for (const path of paths.value) {
      const restored =
        resets.value.has(path) && template.value && sameValue(current[path], template.value[path]);
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
    baseline.value = saved.value ? clone(saved.value) : undefined;
    draft.value = saved.value ? clone(saved.value) : undefined;
    resets.value = new Set();
  }

  /** Restores the template value of one path (FieldReset). */
  function restore(path: keyof Values & Path) {
    if (!draft.value || !template.value) return;
    draft.value = { ...draft.value, [path]: clone(template.value[path]) };
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
