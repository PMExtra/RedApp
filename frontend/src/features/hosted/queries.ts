import { computed, toValue, type MaybeRefOrGetter, type Ref } from "vue";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/vue-query";
import {
  api,
  isAbortError,
  queryKey,
  unwrap,
  uploadWithProgress,
  type Schema,
  type UploadProgress,
} from "@/shared/api";

export type HostedFile = Schema<"HostedFile">;
export type HostedTransfer = Schema<"HostedTransfer">;

/** Transfer progress is first read after 250 ms, then every 750 ms. */
export const TRANSFER_FIRST_POLL_MS = 250;
export const TRANSFER_POLL_MS = 750;

type Name = MaybeRefOrGetter<string>;

/** 32 lowercase hex characters, the format of client-generated IDs. */
export function randomId(): string {
  return Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
}

export function useHostedFiles(vendor: Name, app: Name, page: Ref<number>) {
  return useQuery({
    queryKey: computed(() =>
      queryKey("listHostedFiles", { vendor: toValue(vendor), app: toValue(app), page: page.value }),
    ),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/admin/api/apps/{vendor}/{app}/files", {
          params: {
            path: { vendor: toValue(vendor), app: toValue(app) },
            query: { page: page.value, limit: 25 },
          },
          signal,
        }),
      ),
    placeholderData: keepPreviousData,
  });
}

/** Server-side progress of a running upload or import; polled while `enabled`. */
export function useHostedTransfer(
  vendor: Name,
  app: Name,
  transferId: Ref<string | null>,
  enabled: Ref<boolean>,
) {
  return useQuery({
    queryKey: computed(() =>
      queryKey("getHostedTransfer", {
        vendor: toValue(vendor),
        app: toValue(app),
        transfer_id: transferId.value,
      }),
    ),
    enabled: computed(() => transferId.value !== null && enabled.value),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/admin/api/apps/{vendor}/{app}/files/transfers/{transfer_id}", {
          params: {
            path: {
              vendor: toValue(vendor),
              app: toValue(app),
              transfer_id: transferId.value ?? "",
            },
          },
          signal,
        }),
      ),
    // The transfer may not have started yet or may just have finished (404).
    retry: false,
    staleTime: 0,
    refetchInterval: TRANSFER_POLL_MS,
    refetchIntervalInBackground: true,
  });
}

export type HostedSource = { kind: "upload"; file: File } | { kind: "url"; url: string };

export interface HostedSave {
  path: string;
  source: HostedSource;
  /** Current file ID when replacing. */
  expectedId: string | null;
  transferId: string;
  signal: AbortSignal;
  onProgress?: (progress: UploadProgress) => void;
}

/**
 * Upload (multipart, fields in the order `path`, `expected_id`, `file`) or
 * one-time URL import. Resolves `null` when the user aborted the request.
 */
export function useSaveHostedFile(vendor: Name, app: Name) {
  const queryClient = useQueryClient();
  return useMutation({
    meta: { handledCodes: ["FILE_CONFLICT", "TRANSFER_CANCELLED"] },
    mutationFn: async (input: HostedSave): Promise<HostedFile | null> => {
      const path = { vendor: toValue(vendor), app: toValue(app) };
      try {
        if (input.source.kind === "upload") {
          const form = new FormData();
          form.append("path", input.path);
          if (input.expectedId) form.append("expected_id", input.expectedId);
          form.append("file", input.source.file, input.source.file.name);
          const url =
            `/admin/api/apps/${encodeURIComponent(path.vendor)}/${encodeURIComponent(path.app)}` +
            `/files?transfer_id=${input.transferId}`;
          return await uploadWithProgress<HostedFile>({
            url,
            body: form,
            signal: input.signal,
            onProgress: input.onProgress,
          });
        }
        return await unwrap(
          api.POST("/admin/api/apps/{vendor}/{app}/files/import", {
            params: { path, query: { transfer_id: input.transferId } },
            body: {
              path: input.path,
              url: input.source.url,
              ...(input.expectedId ? { expected_id: input.expectedId } : {}),
            },
            signal: input.signal,
          }),
        );
      } catch (error) {
        if (isAbortError(error)) return null;
        throw error;
      }
    },
    onSettled: () =>
      queryClient.invalidateQueries({
        queryKey: ["listHostedFiles", { vendor: toValue(vendor), app: toValue(app) }],
      }),
  });
}

/** Asks the server to stop a transfer; the original request then fails or aborts. */
export function useCancelHostedTransfer(vendor: Name, app: Name) {
  return useMutation({
    meta: { silent: true },
    mutationFn: (transferId: string) =>
      unwrap(
        api.DELETE("/admin/api/apps/{vendor}/{app}/files/transfers/{transfer_id}", {
          params: {
            path: { vendor: toValue(vendor), app: toValue(app), transfer_id: transferId },
          },
        }),
      ),
  });
}

export function useDeleteHostedFile(vendor: Name, app: Name) {
  const queryClient = useQueryClient();
  return useMutation({
    meta: { handledCodes: ["FILE_NOT_FOUND"] },
    mutationFn: (fileId: string) =>
      unwrap(
        api.DELETE("/admin/api/apps/{vendor}/{app}/files/{file_id}", {
          params: { path: { vendor: toValue(vendor), app: toValue(app), file_id: fileId } },
        }),
      ),
    onSettled: () =>
      queryClient.invalidateQueries({
        queryKey: ["listHostedFiles", { vendor: toValue(vendor), app: toValue(app) }],
      }),
  });
}
