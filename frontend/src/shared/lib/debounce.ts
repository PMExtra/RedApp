import { onScopeDispose, ref, watch, type Ref } from "vue";

/** A copy of `source` that updates `delayMs` after the last change (search boxes). */
export function useDebounced<T>(source: Ref<T>, delayMs: number): Ref<T> {
  const debounced = ref(source.value) as Ref<T>;
  let timer: ReturnType<typeof setTimeout> | undefined;
  watch(source, (value) => {
    clearTimeout(timer);
    timer = setTimeout(() => {
      debounced.value = value;
    }, delayMs);
  });
  onScopeDispose(() => clearTimeout(timer));
  return debounced;
}
