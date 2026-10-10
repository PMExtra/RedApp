import { computed, ref } from "vue";

export const AUTO_REFRESH_INTERVAL_MS = 5_000;

/**
 * State for an "Auto refresh" toggle. Pass `refetchInterval` to `useQuery`;
 * TanStack Query pauses intervals while the tab is hidden and refetches when
 * it becomes visible again.
 */
export function useAutoRefresh(initial = true, intervalMs = AUTO_REFRESH_INTERVAL_MS) {
  const enabled = ref(initial);
  return {
    enabled,
    refetchInterval: computed(() => (enabled.value ? intervalMs : false)),
  };
}
