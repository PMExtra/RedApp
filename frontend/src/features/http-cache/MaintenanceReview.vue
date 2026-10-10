<script setup lang="ts">
import { computed, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useExpired } from "@/features/releases";
import { isApiError, type Schema } from "@/shared/api";
import { useFormat } from "@/shared/i18n";
import { confirm, useCursorPagination } from "@/shared/lib";
import {
  Alert,
  Badge,
  Button,
  CursorPagination,
  DataTable,
  ProgressBar,
  type BadgeTone,
  type DataTableColumn,
} from "@/shared/ui";
import {
  MAINTENANCE_POLL_MS,
  isActive,
  useMaintenanceActions,
  useMaintenanceItems,
  useMaintenanceJob,
  type MaintenanceKind,
  type MaintenancePreview,
} from "./queries";

/**
 * A frozen refresh or cleanup selection: its parameters, the selected files
 * page by page, execution and the result. Polls while it builds or runs.
 */
const props = defineProps<{
  kind: MaintenanceKind;
  vendor: string;
  app: string;
  previewId: string;
  disabled?: boolean;
}>();
const emit = defineEmits<{ discard: []; stale: []; "update:active": [active: boolean] }>();
const { t } = useI18n();
const format = useFormat();
const id = computed(() => props.previewId);
const job = useMaintenanceJob(
  props.kind,
  () => props.vendor,
  () => props.app,
  id,
);
const actions = useMaintenanceActions(
  props.kind,
  () => props.vendor,
  () => props.app,
);
const preview = computed(() => job.data.value);
const active = computed(() => isActive(preview.value));
const pages = useCursorPagination();
const items = useMaintenanceItems(
  props.kind,
  () => props.vendor,
  () => props.app,
  id,
  pages.cursor,
  computed(() => preview.value !== undefined && preview.value.state !== "building"),
  computed(() => (active.value ? MAINTENANCE_POLL_MS : false)),
);
const expired = useExpired(() =>
  preview.value?.state === "ready" ? preview.value.expires_at : undefined,
);

watch(
  () => job.error.value,
  (error) => {
    if (isApiError(error, "PREVIEW_NOT_FOUND")) emit("stale");
  },
);
watch(active, (now) => emit("update:active", now), { immediate: true });
// Per-file results change when the job starts, runs and finishes; a
// background job (refresh) also changes the cached files when it finishes.
watch(
  () => preview.value?.state,
  (now, before) => {
    if (before && now && now !== before && now !== "building") void items.refetch();
    if (before === "running" && (now === "done" || now === "failed")) {
      actions.invalidateResults();
    }
  },
);

const stateTones: Record<MaintenancePreview["state"], BadgeTone> = {
  building: "info",
  ready: "primary",
  running: "info",
  done: "success",
  failed: "danger",
};
const itemTones: Partial<Record<Schema<"MaintenanceItem">["result_status"], BadgeTone>> = {
  retired: "success",
  refreshed: "success",
  not_modified: "success",
  stale_fallback: "warning",
  skipped_accessed: "warning",
  skipped_changed: "warning",
  skipped: "neutral",
  failed: "danger",
};
const columns = computed<DataTableColumn[]>(() => [
  { key: "path", label: t("httpCache.review.columns.path") },
  { key: "size_bytes", label: t("httpCache.review.columns.size"), align: "end" },
  { key: "result_status", label: t("httpCache.review.columns.result") },
]);
const cleanupResult = computed(() => {
  const result = preview.value?.result;
  return result && result.kind === "cleanup" ? result : null;
});
const refreshResult = computed(() => {
  const result = preview.value?.result;
  return result && result.kind === "refresh" ? result : null;
});
const progress = computed(() => {
  const value = preview.value;
  return value ? value.completed_files + value.failed_files : 0;
});

async function execute() {
  const value = preview.value;
  if (!value || value.state !== "ready" || expired.value) return;
  const confirmed = await confirm({
    title: t(`httpCache.review.${props.kind}.confirmTitle`),
    description: t(`httpCache.review.${props.kind}.confirmDescription`, {
      count: value.selected_files,
      size: format.bytes(value.selected_bytes),
    }),
    confirmLabel: t(`httpCache.review.${props.kind}.execute`),
    tone: props.kind === "cleanup" ? "danger" : "default",
  });
  if (!confirmed) return;
  try {
    await actions.execute.mutateAsync(value.id);
  } catch (error) {
    if (isApiError(error, "PREVIEW_NOT_FOUND", "PREVIEW_STALE")) emit("stale");
    else if (isApiError(error, "OPERATION_IN_PROGRESS")) void job.refetch();
  }
}
</script>

<template>
  <section
    class="flex flex-col gap-3 rounded-lg border border-border p-4"
    :aria-label="t(`httpCache.review.${kind}.title`)"
  >
    <div v-if="preview" class="flex flex-col gap-3">
      <div class="flex flex-wrap items-center gap-2">
        <h3 class="text-sm font-semibold">{{ t(`httpCache.review.${kind}.title`) }}</h3>
        <Badge :tone="stateTones[preview.state]" aria-live="polite">
          {{ t(`httpCache.review.states.${preview.state}`) }}
        </Badge>
      </div>
      <p class="text-sm">
        <span class="text-muted">{{ t(`cachePolicy.match.${preview.match.type}`) }}</span>
        <code class="ms-2 font-mono">{{ preview.match.pattern }}</code>
        <template v-if="preview.basis && preview.before">
          ·
          {{
            t("httpCache.review.cutoff", {
              basis: t(`cachePolicy.basis.${preview.basis}`),
              time: format.dateTime(preview.before),
              utc: preview.before,
            })
          }}
        </template>
        · {{ t("httpCache.review.source", { epoch: preview.source_epoch }) }}
      </p>
      <dl class="grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
        <div>
          <dt class="text-xs text-muted">{{ t("httpCache.review.scanned") }}</dt>
          <dd class="font-medium tabular-nums">{{ format.number(preview.scanned_files) }}</dd>
        </div>
        <div>
          <dt class="text-xs text-muted">{{ t("httpCache.review.selected") }}</dt>
          <dd class="font-medium tabular-nums">{{ format.number(preview.selected_files) }}</dd>
        </div>
        <div>
          <dt class="text-xs text-muted">{{ t("httpCache.review.size") }}</dt>
          <dd class="font-medium tabular-nums">{{ format.bytes(preview.selected_bytes) }}</dd>
        </div>
        <div>
          <dt class="text-xs text-muted">{{ t("httpCache.review.active") }}</dt>
          <dd class="font-medium tabular-nums">{{ format.number(preview.active_files) }}</dd>
        </div>
      </dl>
      <p v-if="preview.state === 'building'" class="text-sm text-muted">
        {{ t("httpCache.review.building") }}
      </p>
      <div v-if="preview.state === 'running'" class="flex flex-col gap-1">
        <ProgressBar
          :value="progress"
          :max="Math.max(preview.selected_files, 1)"
          :label="t('httpCache.review.progress')"
        />
        <p class="text-xs text-muted">
          {{
            t("httpCache.review.running", {
              completed: format.number(preview.completed_files),
              failed: format.number(preview.failed_files),
              total: format.number(preview.selected_files),
            })
          }}
        </p>
      </div>
      <Alert v-if="preview.state === 'failed'" tone="danger">
        {{ t("httpCache.review.failed") }}
      </Alert>
      <Alert v-if="cleanupResult" tone="success" :title="t('httpCache.review.cleanup.done')">
        {{
          t("httpCache.review.cleanup.result", {
            retired: format.number(cleanupResult.retired_files),
            selected: format.number(cleanupResult.selected_files),
            size: format.bytes(cleanupResult.retired_bytes),
            accessed: format.number(cleanupResult.skipped_accessed),
            changed: format.number(cleanupResult.skipped_changed),
          })
        }}
      </Alert>
      <Alert v-if="refreshResult" tone="success" :title="t('httpCache.review.refresh.done')">
        {{
          t("httpCache.review.refresh.result", {
            refreshed: format.number(refreshResult.refreshed),
            unchanged: format.number(refreshResult.not_modified),
            stale: format.number(refreshResult.stale_fallback),
            failed: format.number(refreshResult.failed),
            skipped: format.number(refreshResult.skipped),
          })
        }}
      </Alert>

      <template v-if="preview.state !== 'building'">
        <DataTable
          :columns="columns"
          :rows="items.data.value?.items"
          :row-key="(row) => String(row.ordinal)"
          :loading="items.isPending.value"
          :error="items.error.value"
          :caption="t('httpCache.review.files')"
          :empty-text="t('httpCache.review.noFiles')"
          @retry="items.refetch()"
        >
          <template #cell-path="{ row }">
            <span class="font-mono text-xs break-all">{{ row.path }}</span>
          </template>
          <template #cell-size_bytes="{ row }">
            <span class="tabular-nums">{{ format.bytes(row.size_bytes) }}</span>
          </template>
          <template #cell-result_status="{ row }">
            <Badge :tone="itemTones[row.result_status] ?? 'neutral'">
              {{ t(`httpCache.review.items.${row.result_status}`) }}
            </Badge>
            <span v-if="row.error_code" class="ms-2 font-mono text-xs text-muted">
              {{ row.error_code }}
            </span>
          </template>
        </DataTable>
        <p class="text-xs text-muted">{{ t("httpCache.review.pageHint") }}</p>
        <CursorPagination
          :page="pages.page.value"
          :has-previous="pages.hasPrevious.value"
          :has-next="Boolean(items.data.value?.next_cursor)"
          :loading="items.isFetching.value"
          @previous="pages.previous()"
          @next="pages.next(items.data.value?.next_cursor)"
        />
      </template>

      <template v-if="preview.state === 'ready'">
        <p class="text-xs text-muted">
          {{ t("releases.preview.expires", { time: format.dateTime(preview.expires_at) }) }}
        </p>
        <Alert v-if="expired" tone="warning" :title="t('releases.preview.expiredTitle')">
          {{ t("releases.preview.expiredDescription") }}
        </Alert>
        <p class="text-xs text-muted">{{ t(`httpCache.review.${kind}.hint`) }}</p>
      </template>
      <p v-if="kind === 'refresh' && preview.state === 'running'" class="text-xs text-muted">
        {{ t("httpCache.review.refresh.background") }}
      </p>
      <div class="flex flex-wrap gap-2">
        <Button
          v-if="preview.state === 'ready'"
          :variant="kind === 'cleanup' ? 'danger' : 'primary'"
          :loading="actions.execute.isPending.value"
          :disabled="disabled || expired || preview.selected_files === 0"
          @click="execute"
        >
          {{ t(`httpCache.review.${kind}.execute`) }}
        </Button>
        <Button v-if="!active" :disabled="actions.execute.isPending.value" @click="emit('discard')">
          {{ preview.state === "ready" ? t("common.actions.cancel") : t("common.actions.close") }}
        </Button>
      </div>
    </div>
    <p v-else-if="job.isPending.value" class="text-sm text-muted">
      {{ t("common.states.loading") }}
    </p>
  </section>
</template>
