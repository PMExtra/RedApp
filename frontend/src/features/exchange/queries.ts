import { toValue, type MaybeRefOrGetter } from "vue";
import { useMutation, useQueryClient } from "@tanstack/vue-query";
import {
  api,
  queryKey,
  unwrap,
  uploadWithProgress,
  useRevisionedMutation,
  type Schema,
  type UploadProgress,
} from "@/shared/api";

export type ExportRequest = Schema<"ExportRequest">;
export type ImportPreview = Schema<"ImportPreview">;
export type ImportItem = Schema<"ImportItem">;
export type ImportChoice = Schema<"ImportChoice">;
export type ImportResult = Schema<"ImportResult">;
export type CopyRequest = Schema<"CopyRequest">;
export type ExchangeMode = Schema<"ExchangeMode">;

const FALLBACK_NAME = "redapp-configuration.zip";

/** File name from `Content-Disposition: attachment; filename="…"`. */
function fileName(disposition: string | null): string {
  const match = /filename="([^"]+)"/.exec(disposition ?? "");
  return match?.[1] ?? FALLBACK_NAME;
}

/** Saves a blob through a temporary object URL (no inline script, CSP-safe). */
function download(blob: Blob, name: string): void {
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = name;
  link.rel = "noopener";
  document.body.append(link);
  link.click();
  link.remove();
  setTimeout(() => {
    URL.revokeObjectURL(url);
  }, 1000);
}

/** Exports the selection as a ZIP and starts the browser download. */
export function useExport() {
  return useMutation({
    mutationFn: async (body: ExportRequest) => {
      const result = await api.POST("/admin/api/configuration/export", {
        body,
        parseAs: "blob",
      });
      const blob = await unwrap(Promise.resolve(result));
      download(blob, fileName(result.response.headers.get("Content-Disposition")));
    },
  });
}

/**
 * Uploads a package with the current choices and returns a new preview.
 * Choices go before the file so the server reads them first.
 */
export function previewImport(
  file: File,
  choices: ImportChoice[],
  options: { signal?: AbortSignal; onProgress?: (progress: UploadProgress) => void } = {},
): Promise<ImportPreview> {
  const body = new FormData();
  body.append("choices", new Blob([JSON.stringify(choices)], { type: "application/json" }));
  body.append("file", file);
  return uploadWithProgress<ImportPreview>({
    url: "/admin/api/configuration/import/preview",
    body,
    ...options,
  });
}

/** Executes a ready preview; every query is refreshed afterwards. */
export function useExecuteImport() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ id, trust }: { id: string; trust: boolean }) =>
      unwrap(
        api.POST("/admin/api/configuration/import/{preview_id}/execute", {
          params: { path: { preview_id: id } },
          body: { trust_instructions: trust },
        }),
      ),
    onSuccess: () => client.invalidateQueries(),
  });
}

/**
 * Copies an application (If-Match: the source revision the user saw). The
 * response is the new application, so it never replaces the source's cache;
 * after a 409 call `reloadSource()` to refetch the source.
 */
export function useCopyApp(
  source: MaybeRefOrGetter<{ vendor_id: string; id: string; revision: number } | undefined>,
) {
  const client = useQueryClient();
  const sourceKey = () => {
    const app = toValue(source);
    return app ? queryKey("getApp", { vendor: app.vendor_id, app: app.id }) : undefined;
  };
  const mutation = useRevisionedMutation<Schema<"App">, CopyRequest>({
    revision: () => toValue(source)?.revision,
    mutationFn: (body, ifMatch) => {
      const app = toValue(source);
      if (!app) return Promise.reject(new Error("no source application"));
      return unwrap(
        api.POST("/admin/api/apps/{vendor}/{app}/copy", {
          params: { path: { vendor: app.vendor_id, app: app.id }, header: { "If-Match": ifMatch } },
          body,
        }),
      );
    },
    onSuccess: () => {
      for (const key of [queryKey("listApps"), queryKey("listVendors")]) {
        void client.invalidateQueries({ queryKey: key });
      }
    },
  });
  async function reloadSource(): Promise<void> {
    mutation.dismissConflict();
    const key = sourceKey();
    if (key) await client.refetchQueries({ queryKey: key, exact: true });
  }
  return { ...mutation, reloadSource };
}
