import { computed, ref, toValue, watch, type MaybeRefOrGetter } from "vue";
import { useForm } from "vee-validate";
import type { z } from "zod";
import type { Schema } from "@/shared/api";
import { useDirtyGuard, zodSchema } from "@/shared/forms";

/**
 * The parts of an app, vendor or category configuration that overlay editing
 * needs: the template (`defaults`), the effective spec and each path's origin.
 */
export interface OverlayConfiguration {
  revision: number;
  template_ref: string | null;
  template_missing: boolean;
  defaults: object | null;
  effective: object;
  fields: Partial<Record<string, Schema<"FieldOrigin">>>;
}

/** Sparse overlay patch (`set` dotted paths, `unset` paths back to the template). */
export interface OverlayPatch {
  set: Record<string, unknown>;
  unset: string[];
}

/** The value at a dotted configuration path (`name.en`, `http_policy.rules`). */
export function getLeaf(source: unknown, path: string): unknown {
  let value: unknown = source;
  for (const key of path.split(".")) {
    if (typeof value !== "object" || value === null) return undefined;
    value = (value as Record<string, unknown>)[key];
  }
  return value;
}

/** Writes `value` at a dotted path, creating intermediate objects. */
export function setLeaf(target: Record<string, unknown>, path: string, value: unknown): void {
  const keys = path.split(".");
  const last = keys.pop() ?? path;
  let node = target;
  for (const key of keys) {
    const next = node[key];
    if (typeof next !== "object" || next === null) node[key] = {};
    node = node[key] as Record<string, unknown>;
  }
  node[last] = value;
}

function copy<T>(value: T): T {
  return value === undefined ? value : (JSON.parse(JSON.stringify(value)) as T);
}

/** Short text of a template value for the reset tooltip. */
function displayValue(value: unknown): string {
  if (typeof value === "string") return value.length > 80 ? `${value.slice(0, 80)}…` : value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  if (Array.isArray(value) && value.every((item) => typeof item === "string")) {
    return value.join(", ");
  }
  return "";
}

function same(a: unknown, b: unknown): boolean {
  return JSON.stringify(a) === JSON.stringify(b);
}

export interface OverlayFormOptions<Values extends Record<string, unknown>> {
  /** The cached configuration (from `useAppConfiguration` and friends). */
  configuration: MaybeRefOrGetter<OverlayConfiguration | undefined>;
  /** Overlay paths this form edits; the draft holds exactly these leaves. */
  paths: MaybeRefOrGetter<readonly string[]>;
  /** Validation of the draft (nested like the spec, e.g. `{ name: { en } }`). */
  schema: z.ZodType<Values>;
  /** Ask before leaving the page while the draft has changes (default true). */
  guard?: boolean;
}

/**
 * A vee-validate form over some paths of a configuration overlay.
 *
 * - The draft starts from `effective` and follows the server while unchanged.
 *   While it has changes, a refetch (or a 409 reload) keeps the user's draft.
 * - `reset(path)` restores the template value; if the path was overridden it is
 *   sent as `unset`, so it follows the template again.
 * - `patch(values)` contains only edited or reset paths, so untouched fields
 *   keep their inheritance and other sections' fields are never overwritten.
 * - After a successful save call `load(response)` to make it the new baseline.
 */
export function useOverlayForm<Values extends Record<string, unknown>>(
  options: OverlayFormOptions<Values>,
) {
  // Keep values of fields that are hidden (provider-gated or collapsed) in the draft.
  const form = useForm<Values>({
    validationSchema: zodSchema(options.schema),
    keepValuesOnUnmount: true,
  });
  const unsets = ref<string[]>([]);
  const configuration = computed(() => toValue(options.configuration));
  const paths = computed(() => toValue(options.paths));

  const dirty = computed(() => form.meta.value.dirty || unsets.value.length > 0);
  if (options.guard !== false) useDirtyGuard(dirty);

  function snapshot(config: OverlayConfiguration): Values {
    const values: Record<string, unknown> = {};
    for (const path of paths.value) setLeaf(values, path, copy(getLeaf(config.effective, path)));
    return values as Values;
  }

  /** Replaces the draft and its baseline with `config` (after load or save). */
  function load(config: OverlayConfiguration): void {
    form.resetForm({ values: snapshot(config) as never });
    unsets.value = [];
  }

  /** Throws away the draft and shows the latest cached configuration. */
  function discard(): void {
    if (configuration.value) load(configuration.value);
  }

  watch(
    [configuration, paths],
    ([config]) => {
      if (config && !dirty.value) load(config);
    },
    { immediate: true },
  );

  const linked = computed(() => configuration.value?.template_ref != null);

  function origin(path: string): Schema<"FieldOrigin"> | undefined {
    return configuration.value?.fields[path];
  }

  function templateValue(path: string): unknown {
    return getLeaf(configuration.value?.defaults, path);
  }

  function baseline(path: string): unknown {
    return getLeaf(form.meta.value.initialValues, path);
  }

  /** The draft differs from the saved value, or the path is queued for reset. */
  function modified(path: string): boolean {
    return unsets.value.includes(path) || !same(getLeaf(form.values, path), baseline(path));
  }

  /** Restores the template value of `path` (see FieldReset). */
  function reset(path: string): void {
    const config = configuration.value;
    if (!config?.defaults) return;
    form.setFieldValue(path as never, copy(templateValue(path)) as never);
    if (origin(path)?.source === "custom" && !unsets.value.includes(path)) {
      unsets.value = [...unsets.value, path];
    }
  }

  /** Props for `FieldReset` next to the control of `path`. */
  function resetBinding(path: string, format: (value: unknown) => string = displayValue) {
    const value = templateValue(path);
    return {
      linked: linked.value,
      origin: origin(path),
      modified: modified(path),
      templateValue: value === undefined ? undefined : format(value),
    };
  }

  function patch(values: Record<string, unknown>): OverlayPatch {
    const set: Record<string, unknown> = {};
    const unset: string[] = [];
    for (const path of paths.value) {
      const value = getLeaf(values, path);
      if (unsets.value.includes(path) && same(value, templateValue(path))) {
        unset.push(path);
      } else if (!same(value, baseline(path))) {
        set[path] = copy(value);
      }
    }
    return { set, unset };
  }

  return {
    form,
    dirty,
    linked,
    load,
    discard,
    origin,
    templateValue,
    modified,
    reset,
    resetBinding,
    patch,
  };
}

/** `true` when a patch would change nothing. */
export function isEmptyPatch(patch: OverlayPatch): boolean {
  return Object.keys(patch.set).length === 0 && patch.unset.length === 0;
}
