import { computed, ref } from "vue";

/**
 * Client state for cursor-paginated lists (`limit`/`cursor` → `next_cursor`).
 * The server only returns a next cursor, so previous pages are remembered as a
 * stack. Put `cursor` into the query key; call `reset()` when filters change.
 */
export function useCursorPagination() {
  const stack = ref<(string | null)[]>([null]);
  const index = ref(0);

  const cursor = computed(() => stack.value[index.value] ?? null);

  return {
    /** Cursor of the current page; `null` for the first page. */
    cursor,
    /** 1-based number of the current page. */
    page: computed(() => index.value + 1),
    hasPrevious: computed(() => index.value > 0),
    next(nextCursor: string | null | undefined) {
      if (!nextCursor) return;
      stack.value = [...stack.value.slice(0, index.value + 1), nextCursor];
      index.value++;
    },
    previous() {
      if (index.value > 0) index.value--;
    },
    reset() {
      stack.value = [null];
      index.value = 0;
    },
  };
}

/** Parses a `?page=` value; anything but a positive integer is page 1. */
export function parsePage(value: unknown): number {
  if (typeof value !== "string" || !/^[1-9]\d{0,8}$/.test(value)) return 1;
  return Number(value);
}
