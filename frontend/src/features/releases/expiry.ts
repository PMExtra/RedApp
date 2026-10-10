import { onScopeDispose, ref, watch, type Ref } from "vue";

// setTimeout stores the delay as a signed 32-bit integer.
const MAX_DELAY_MS = 2 ** 31 - 1;

/**
 * Becomes `true` when the RFC 3339 deadline passes (previews expire after 10
 * minutes on the server). Lets the page disable execution before the server
 * would reject it with `PREVIEW_NOT_FOUND`.
 */
export function useExpired(deadline: () => string | null | undefined): Readonly<Ref<boolean>> {
  const expired = ref(false);
  let timer: ReturnType<typeof setTimeout> | undefined;
  watch(
    deadline,
    (value) => {
      clearTimeout(timer);
      timer = undefined;
      if (!value) {
        expired.value = false;
        return;
      }
      const remaining = new Date(value).getTime() - Date.now();
      expired.value = !(remaining > 0);
      if (remaining > 0) {
        timer = setTimeout(
          () => {
            expired.value = new Date(value).getTime() <= Date.now();
          },
          Math.min(remaining, MAX_DELAY_MS),
        );
      }
    },
    { immediate: true },
  );
  onScopeDispose(() => clearTimeout(timer));
  return expired;
}
