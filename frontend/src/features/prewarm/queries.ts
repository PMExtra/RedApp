import { computed, toValue, type MaybeRefOrGetter, type Ref } from "vue";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/vue-query";
import { api, queryKey, unwrap, type Schema } from "@/shared/api";

export type PrewarmJob = Schema<"PrewarmJob">;
export type PrewarmLimits = Schema<"PrewarmLimits">;
export type PrewarmOptions = Schema<"PrewarmOptions">;
export type PrewarmStartRequest = Schema<"PrewarmStartRequest">;

/** The admin UI polls a running job every 1.5 seconds (`x-polling`). */
export const PREWARM_POLL_MS = 1_500;

type Name = MaybeRefOrGetter<string>;

function storageKey(vendor: string, app: string): string {
  return `redapp-prewarm-job:${vendor}/${app}`;
}

/** The last job started from this browser, so a reload resumes watching it. */
export function loadJobId(vendor: string, app: string): string | null {
  try {
    const value = localStorage.getItem(storageKey(vendor, app));
    return value && /^[0-9a-f]{32}$/.test(value) ? value : null;
  } catch {
    return null;
  }
}

export function storeJobId(vendor: string, app: string, id: string | null): void {
  try {
    if (id) localStorage.setItem(storageKey(vendor, app), id);
    else localStorage.removeItem(storageKey(vendor, app));
  } catch {
    // Resuming after a reload is optional.
  }
}

export function usePrewarmOptions(vendor: Name, app: Name) {
  return useQuery({
    queryKey: computed(() =>
      queryKey("getPrewarmOptions", { vendor: toValue(vendor), app: toValue(app) }),
    ),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/admin/api/apps/{vendor}/{app}/prewarm/options", {
          params: { path: { vendor: toValue(vendor), app: toValue(app) } },
          signal,
        }),
      ),
  });
}

function jobKey(vendor: string, app: string, id: string | null) {
  return queryKey("getPrewarmJob", { vendor, app, job_id: id });
}

export function usePrewarmJob(vendor: Name, app: Name, id: Ref<string | null>) {
  return useQuery({
    queryKey: computed(() => jobKey(toValue(vendor), toValue(app), id.value)),
    enabled: computed(() => id.value !== null),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/admin/api/apps/{vendor}/{app}/prewarm/jobs/{job_id}", {
          params: {
            path: { vendor: toValue(vendor), app: toValue(app), job_id: id.value ?? "" },
          },
          signal,
        }),
      ),
    staleTime: 0,
    refetchInterval: (query) => (query.state.data?.state === "running" ? PREWARM_POLL_MS : false),
  });
}

export function usePrewarmItems(
  vendor: Name,
  app: Name,
  id: Ref<string | null>,
  page: Ref<number>,
  running: Ref<boolean>,
) {
  return useQuery({
    queryKey: computed(() =>
      queryKey("listPrewarmItems", {
        vendor: toValue(vendor),
        app: toValue(app),
        job_id: id.value,
        page: page.value,
      }),
    ),
    enabled: computed(() => id.value !== null),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/admin/api/apps/{vendor}/{app}/prewarm/jobs/{job_id}/items", {
          params: {
            path: { vendor: toValue(vendor), app: toValue(app), job_id: id.value ?? "" },
            query: { page: page.value, limit: 25 },
          },
          signal,
        }),
      ),
    staleTime: 0,
    placeholderData: keepPreviousData,
    refetchInterval: computed(() => (running.value ? PREWARM_POLL_MS : false)),
  });
}

/** Start, cancel and retry; every response is the job and seeds its query. */
export function usePrewarmActions(vendor: Name, app: Name) {
  const queryClient = useQueryClient();
  const path = () => ({ vendor: toValue(vendor), app: toValue(app) });
  const remember = (job: PrewarmJob) => {
    queryClient.setQueryData(jobKey(path().vendor, path().app, job.id), job);
  };
  const start = useMutation({
    meta: { handledCodes: ["PREWARM_BUSY"] },
    mutationFn: (body: PrewarmStartRequest) =>
      unwrap(
        api.POST("/admin/api/apps/{vendor}/{app}/prewarm/jobs", { params: { path: path() }, body }),
      ),
    onSuccess: remember,
  });
  const cancel = useMutation({
    meta: { handledCodes: ["JOB_NOT_FOUND"] },
    mutationFn: (id: string) =>
      unwrap(
        api.POST("/admin/api/apps/{vendor}/{app}/prewarm/jobs/{job_id}/cancel", {
          params: { path: { ...path(), job_id: id } },
        }),
      ),
    onSuccess: remember,
  });
  const retry = useMutation({
    meta: { handledCodes: ["PREWARM_BUSY", "JOB_NOT_FOUND"] },
    mutationFn: ({ id, requestId }: { id: string; requestId: string }) =>
      unwrap(
        api.POST("/admin/api/apps/{vendor}/{app}/prewarm/jobs/{job_id}/retry", {
          params: { path: { ...path(), job_id: id } },
          body: { request_id: requestId },
        }),
      ),
    onSuccess: remember,
  });
  return { start, cancel, retry };
}
