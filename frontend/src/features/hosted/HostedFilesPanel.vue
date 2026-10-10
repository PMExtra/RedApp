<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { Download, Pencil, Trash2 } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { isApiError, type UploadProgress } from "@/shared/api";
import { useDirtyGuard } from "@/shared/forms";
import { useFormat } from "@/shared/i18n";
import { confirm, distributionPath, randomId, toast } from "@/shared/lib";
import {
  Alert,
  Button,
  Card,
  DataTable,
  Field,
  FilePicker,
  IconButton,
  Input,
  Pagination,
  ProgressBar,
  RadioGroup,
  RelativeTime,
  type DataTableColumn,
  type RadioOption,
} from "@/shared/ui";
import {
  TRANSFER_FIRST_POLL_MS,
  useCancelHostedTransfer,
  useDeleteHostedFile,
  useHostedFiles,
  useHostedTransfer,
  useSaveHostedFile,
  type HostedFile,
} from "./queries";

/**
 * Files of a hosted application: upload with progress or import once from a
 * URL (create, or replace a specific file version), list, download, delete.
 */
const props = defineProps<{
  vendor: string;
  app: string;
  /** Deleted applications keep their files readable and deletable only. */
  deleted?: boolean;
}>();
const { t } = useI18n();
const format = useFormat();
// A literal URL in a template attribute confuses vue-tsc; bind it.
const URL_PLACEHOLDER = "https://downloads.example.com/setup.exe";

// ---- list
const page = ref(1);
const files = useHostedFiles(
  () => props.vendor,
  () => props.app,
  page,
);
const remove = useDeleteHostedFile(
  () => props.vendor,
  () => props.app,
);
const columns = computed<DataTableColumn[]>(() => [
  { key: "path", label: t("hosted.list.path") },
  { key: "size_bytes", label: t("hosted.list.size"), align: "end" },
  { key: "sha256", label: t("hosted.list.sha256") },
  { key: "created_at", label: t("hosted.list.created") },
  { key: "actions", label: t("hosted.list.actions"), hideLabel: true, align: "end" },
]);

async function deleteFile(file: HostedFile) {
  const confirmed = await confirm({
    title: t("hosted.delete.title"),
    description: t("hosted.delete.description", { path: file.path }),
    confirmLabel: t("common.actions.delete"),
    tone: "danger",
  });
  if (!confirmed) return;
  remove.mutate(file.id, {
    onSuccess: () =>
      toast({ tone: "success", title: t("hosted.delete.done", { path: file.path }) }),
    onError: (error) => {
      if (isApiError(error, "FILE_NOT_FOUND")) {
        toast({ tone: "info", title: t("hosted.delete.gone", { path: file.path }) });
      }
    },
  });
}

// ---- add or replace
const mode = ref<"upload" | "url">("upload");
const path = ref("");
const url = ref("");
const chosen = ref<File[]>([]);
const replacing = ref<HostedFile | null>(null);
const submitted = ref(false);
const conflict = ref(false);

const modeOptions = computed<RadioOption[]>(() => [
  { value: "upload", label: t("hosted.add.upload") },
  { value: "url", label: t("hosted.add.import") },
]);
const modeValue = computed({
  get: () => mode.value,
  set: (value: string | undefined) => {
    if (value === "upload" || value === "url") mode.value = value;
  },
});
watch(chosen, (list) => {
  const file = list[0];
  if (file && !path.value && !replacing.value) path.value = file.name;
});

// HostedImportRequest.url: absolute http(s), at most 8192 characters, no
// credentials or fragment; the query is allowed.
function importUrlValid(value: string): boolean {
  if (value.length > 8192 || !/^https?:\/\/\S+$/i.test(value) || value.includes("#")) return false;
  try {
    const parsed = new URL(value);
    return parsed.host !== "" && parsed.username === "" && parsed.password === "";
  } catch {
    return false;
  }
}

const normalizedPath = computed(() => path.value.trim().replace(/^\/+/, ""));
const pathError = computed(() => {
  if (!submitted.value) return undefined;
  if (!normalizedPath.value) return t("hosted.add.pathRequired");
  return new TextEncoder().encode(normalizedPath.value).length > 4096
    ? t("hosted.add.pathTooLong")
    : undefined;
});
const urlError = computed(() => {
  if (!submitted.value || mode.value !== "url") return undefined;
  return importUrlValid(url.value.trim()) ? undefined : t("hosted.add.urlInvalid");
});
const fileError = computed(() =>
  submitted.value && mode.value === "upload" && chosen.value.length === 0
    ? t("hosted.add.fileRequired")
    : undefined,
);

function startReplace(file: HostedFile) {
  replacing.value = file;
  path.value = file.path;
  conflict.value = false;
}

function resetForm() {
  replacing.value = null;
  path.value = "";
  url.value = "";
  chosen.value = [];
  submitted.value = false;
}

// ---- transfer
const save = useSaveHostedFile(
  () => props.vendor,
  () => props.app,
);
const cancelTransfer = useCancelHostedTransfer(
  () => props.vendor,
  () => props.app,
);
const transferId = ref<string | null>(null);
const polling = ref(false);
const sent = ref<UploadProgress | null>(null);
let controller: AbortController | undefined;
let firstPoll: ReturnType<typeof setTimeout> | undefined;
const transfer = useHostedTransfer(
  () => props.vendor,
  () => props.app,
  transferId,
  polling,
);
const busy = computed(() => transferId.value !== null);
useDirtyGuard(
  () => busy.value || Boolean(path.value.trim() || url.value.trim() || chosen.value.length),
);

const progress = computed(() => {
  const server = transferId.value ? transfer.data.value : undefined;
  if (server?.state === "committing") {
    return { value: null, text: t("hosted.progress.committing") };
  }
  // The server counts stored file bytes; the browser counts request bytes
  // (multipart overhead included), so prefer the server once it reports.
  const local = mode.value === "upload" ? sent.value : null;
  const bytes = server ? server.bytes : (local?.loaded ?? 0);
  const total = server ? server.total_bytes : (local?.total ?? null);
  return {
    value: total ? Math.min(100, (bytes / total) * 100) : null,
    text: total
      ? t("hosted.progress.of", { done: format.bytes(bytes), total: format.bytes(total) })
      : t("hosted.progress.bytes", { done: format.bytes(bytes) }),
  };
});

function stopTransfer() {
  clearTimeout(firstPoll);
  firstPoll = undefined;
  polling.value = false;
  transferId.value = null;
  controller = undefined;
  sent.value = null;
}

async function submit() {
  submitted.value = true;
  conflict.value = false;
  if (pathError.value || urlError.value || fileError.value || busy.value || props.deleted) return;
  const file = chosen.value[0];
  const id = randomId();
  controller = new AbortController();
  transferId.value = id;
  sent.value = file ? { loaded: 0, total: file.size } : null;
  firstPoll = setTimeout(() => {
    polling.value = true;
  }, TRANSFER_FIRST_POLL_MS);
  try {
    const saved = await save.mutateAsync({
      path: normalizedPath.value,
      source:
        mode.value === "upload" && file
          ? { kind: "upload", file }
          : { kind: "url", url: url.value.trim() },
      expectedId: replacing.value?.id ?? null,
      transferId: id,
      signal: controller.signal,
      onProgress: (value) => {
        if (transferId.value === id) sent.value = value;
      },
    });
    if (saved) {
      toast({ tone: "success", title: t("hosted.add.saved", { path: saved.path }) });
      resetForm();
    } else {
      toast({ tone: "info", title: t("hosted.progress.cancelled") });
    }
  } catch (error) {
    if (isApiError(error, "TRANSFER_CANCELLED")) {
      toast({ tone: "info", title: t("hosted.progress.cancelled") });
    } else if (isApiError(error, "FILE_CONFLICT")) {
      conflict.value = true;
    }
  } finally {
    if (transferId.value === id) stopTransfer();
  }
}

function cancel() {
  const id = transferId.value;
  if (!id) return;
  cancelTransfer.mutate(id, {
    onSettled: () => controller?.abort(),
  });
}

onBeforeUnmount(() => {
  controller?.abort();
  clearTimeout(firstPoll);
});
</script>

<template>
  <div class="flex flex-col gap-6">
    <Alert v-if="deleted" tone="warning">{{ t("hosted.deleted") }}</Alert>
    <Card
      v-else
      :title="replacing ? t('hosted.add.replaceTitle') : t('hosted.add.title')"
      :description="t('hosted.add.description')"
    >
      <form class="flex flex-col gap-4" novalidate @submit.prevent="submit">
        <Alert v-if="replacing" tone="info">
          {{ t("hosted.add.replacing", { path: replacing.path }) }}
          <template #actions>
            <Button size="sm" :disabled="busy" @click="resetForm">
              {{ t("hosted.add.cancelReplace") }}
            </Button>
          </template>
        </Alert>
        <Alert v-if="conflict" tone="warning" :title="t('hosted.add.conflictTitle')">
          {{ t("hosted.add.conflict") }}
        </Alert>
        <fieldset class="flex flex-col gap-4" :disabled="busy">
          <fieldset class="flex flex-col gap-2">
            <legend class="mb-1 text-sm font-medium">{{ t("hosted.add.source") }}</legend>
            <RadioGroup v-model="modeValue" :options="modeOptions" orientation="horizontal" />
          </fieldset>
          <div v-if="mode === 'upload'" class="flex flex-col gap-1.5">
            <span id="hosted-file-label" class="text-sm font-medium">{{
              t("hosted.add.file")
            }}</span>
            <FilePicker v-model="chosen" :disabled="busy" aria-labelledby="hosted-file-label" />
            <p v-if="fileError" class="text-xs text-danger">{{ fileError }}</p>
          </div>
          <template v-else>
            <Field
              v-slot="{ control }"
              :label="t('hosted.add.url')"
              :description="t('hosted.add.urlHint')"
              :error="urlError"
              required
            >
              <Input
                v-bind="control"
                v-model="url"
                type="url"
                inputmode="url"
                autocomplete="off"
                spellcheck="false"
                maxlength="4096"
                :placeholder="URL_PLACEHOLDER"
              />
            </Field>
          </template>
          <Field
            v-slot="{ control }"
            :label="t('hosted.add.path')"
            :description="t('hosted.add.pathHint')"
            :error="pathError"
            required
          >
            <Input
              v-bind="control"
              v-model="path"
              class="font-mono"
              autocomplete="off"
              spellcheck="false"
              maxlength="4096"
              placeholder="tools/setup.exe"
              :readonly="replacing !== null"
            />
          </Field>
        </fieldset>
        <div
          v-if="busy"
          class="flex flex-col gap-2 rounded-lg bg-surface-sunken p-3"
          role="status"
          aria-live="polite"
        >
          <ProgressBar :value="progress.value" :label="t('hosted.progress.label')" />
          <div class="flex flex-wrap items-center justify-between gap-2 text-sm">
            <span class="tabular-nums">{{ progress.text }}</span>
            <Button size="sm" :loading="cancelTransfer.isPending.value" @click="cancel">
              {{ t("hosted.progress.cancel") }}
            </Button>
          </div>
        </div>
        <div class="flex flex-wrap gap-2">
          <Button type="submit" variant="primary" :loading="busy">
            {{ replacing ? t("hosted.add.replace") : t("hosted.add.save") }}
          </Button>
          <Button v-if="!busy && !replacing" @click="resetForm">
            {{ t("hosted.add.clear") }}
          </Button>
        </div>
      </form>
    </Card>

    <Card :title="t('hosted.list.title')" :description="t('hosted.list.description')">
      <div class="flex flex-col gap-3">
        <DataTable
          :columns="columns"
          :rows="files.data.value?.items"
          :row-key="(row) => row.id"
          :loading="files.isPending.value"
          :error="files.error.value"
          :caption="t('hosted.list.title')"
          :empty-text="t('hosted.list.empty')"
          @retry="files.refetch()"
        >
          <template #cell-path="{ row }">
            <a
              :href="distributionPath(vendor, app, row.path)"
              download
              class="inline-flex items-center gap-1 font-mono text-xs break-all text-primary hover:underline focus-ring"
            >
              <Download class="size-3.5 shrink-0" aria-hidden="true" />
              {{ row.path }}
            </a>
          </template>
          <template #cell-size_bytes="{ row }">
            <span class="tabular-nums">{{ format.bytes(row.size_bytes) }}</span>
          </template>
          <template #cell-sha256="{ row }">
            <span class="font-mono text-xs" :title="row.sha256"
              >{{ row.sha256.slice(0, 12) }}…</span
            >
          </template>
          <template #cell-created_at="{ row }">
            <RelativeTime :value="row.created_at" />
          </template>
          <template #cell-actions="{ row }">
            <div class="flex justify-end gap-1">
              <IconButton
                v-if="!deleted"
                size="sm"
                :label="t('hosted.list.replace', { path: row.path })"
                :disabled="busy"
                @click="startReplace(row)"
              >
                <Pencil aria-hidden="true" />
              </IconButton>
              <IconButton
                size="sm"
                :label="t('hosted.list.delete', { path: row.path })"
                :disabled="busy || remove.isPending.value"
                @click="deleteFile(row)"
              >
                <Trash2 aria-hidden="true" />
              </IconButton>
            </div>
          </template>
        </DataTable>
        <Pagination
          v-if="(files.data.value?.total_pages ?? 1) > 1"
          v-model:page="page"
          :total="files.data.value?.total ?? 0"
          :page-size="files.data.value?.limit ?? 25"
        />
      </div>
    </Card>
  </div>
</template>
