import { computed, onBeforeUnmount, ref, type MaybeRefOrGetter } from "vue";
import { useI18n } from "vue-i18n";
import type { UploadProgress } from "@/shared/api";
import { useFormat } from "@/shared/i18n";
import { randomId } from "@/shared/lib";
import {
  TRANSFER_FIRST_POLL_MS,
  useCancelHostedTransfer,
  useHostedTransfer,
  useSaveHostedFile,
  type HostedFile,
  type HostedSave,
} from "./queries";

type Name = MaybeRefOrGetter<string>;
export type TransferRequest = Pick<HostedSave, "path" | "source" | "expectedId">;

/**
 * One upload or URL import at a time, with progress: the browser's upload
 * progress until the server reports, then the server's (polled after
 * TRANSFER_FIRST_POLL_MS). The request is aborted when the component unmounts.
 */
export function useFileTransfer(vendor: Name, app: Name) {
  const { t } = useI18n();
  const format = useFormat();
  const save = useSaveHostedFile(vendor, app);
  const cancelRequest = useCancelHostedTransfer(vendor, app);
  const transferId = ref<string | null>(null);
  const polling = ref(false);
  const sent = ref<UploadProgress | null>(null);
  let controller: AbortController | undefined;
  let firstPoll: ReturnType<typeof setTimeout> | undefined;
  const transfer = useHostedTransfer(vendor, app, transferId, polling);

  const active = computed(() => transferId.value !== null);
  const server = computed(() => (transferId.value ? transfer.data.value : undefined));
  /** The bytes are in and the server is storing the file: it can no longer be cancelled. */
  const committing = computed(() => server.value?.state === "committing");

  const progress = computed(() => {
    if (committing.value) return { value: null, text: t("hosted.progress.committing") };
    // The server counts stored file bytes; the browser counts request bytes
    // (multipart overhead included), so prefer the server once it reports.
    const bytes = server.value ? server.value.bytes : (sent.value?.loaded ?? 0);
    const total = server.value ? server.value.total_bytes : (sent.value?.total ?? null);
    return {
      value: total ? Math.min(100, (bytes / total) * 100) : null,
      text: total
        ? t("hosted.progress.of", { done: format.bytes(bytes), total: format.bytes(total) })
        : t("hosted.progress.bytes", { done: format.bytes(bytes) }),
    };
  });

  function stop() {
    clearTimeout(firstPoll);
    firstPoll = undefined;
    polling.value = false;
    transferId.value = null;
    controller = undefined;
    sent.value = null;
  }

  /** Resolves the saved file, or `null` when the transfer was aborted; rejects like the request. */
  async function run(request: TransferRequest): Promise<HostedFile | null> {
    const id = randomId();
    const source = request.source;
    controller = new AbortController();
    transferId.value = id;
    sent.value = source.kind === "upload" ? { loaded: 0, total: source.file.size } : null;
    firstPoll = setTimeout(() => {
      polling.value = true;
    }, TRANSFER_FIRST_POLL_MS);
    try {
      return await save.mutateAsync({
        ...request,
        transferId: id,
        signal: controller.signal,
        onProgress: (value) => {
          if (transferId.value === id && source.kind === "upload") sent.value = value;
        },
      });
    } finally {
      if (transferId.value === id) stop();
    }
  }

  function cancel() {
    const id = transferId.value;
    if (!id || committing.value) return;
    cancelRequest.mutate(id, { onSettled: () => controller?.abort() });
  }

  onBeforeUnmount(() => {
    controller?.abort();
    clearTimeout(firstPoll);
  });

  return {
    active,
    committing,
    progress,
    run,
    cancel,
    cancelling: computed(() => cancelRequest.isPending.value),
  };
}
