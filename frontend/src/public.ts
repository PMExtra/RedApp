import { shallowRef, ref, watch, onUnmounted, type Ref } from "vue";
import { ApiError, isCancellation, fetchResponse } from "./api";
export async function publicFetch<T>(
  url: string,
  signal?: AbortSignal,
): Promise<T> {
  const response = await fetchResponse(url, {
    credentials: "omit",
    cache: "no-store",
    signal,
  });
  let data: any;
  try {
    data = await response.json();
  } catch {
    if (signal?.aborted) throw new DOMException("Cancelled", "AbortError");
    throw new ApiError({ code: "INVALID_RESPONSE" }, response.status);
  }
  if (signal?.aborted) throw new DOMException("Cancelled", "AbortError");
  if (!response.ok)
    throw new ApiError(data?.error || "Request failed", response.status);
  return data as T;
}
export function usePublicResource<T>(
  url: Ref<string>,
  valid?: (value: T) => boolean,
) {
  const data = shallowRef<T>(),
    error = shallowRef<unknown>(),
    loading = ref(false);
  let ticket = 0,
    controller: AbortController | undefined;
  async function refresh() {
    controller?.abort();
    const request = new AbortController(),
      attempt = ++ticket;
    controller = request;
    loading.value = true;
    error.value = undefined;
    if (!url.value) {
      controller = undefined;
      loading.value = false;
      return;
    }
    try {
      const value = await publicFetch<T>(url.value, request.signal);
      if (valid && !valid(value))
        throw new ApiError({ code: "INVALID_RESPONSE" }, 200);
      if (attempt === ticket) data.value = value;
    } catch (e) {
      if (attempt === ticket && !isCancellation(e)) error.value = e;
    } finally {
      if (attempt === ticket) {
        controller = undefined;
        loading.value = false;
      }
    }
  }
  watch(
    url,
    () => {
      data.value = undefined;
      void refresh();
    },
    { immediate: true, flush: "sync" },
  );
  onUnmounted(() => {
    ticket++;
    controller?.abort();
  });
  return { data, error, loading, refresh };
}
