<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import type { Schema } from "@/shared/api";
import { useFormat } from "@/shared/i18n";
import {
  Badge,
  Button,
  DataTable,
  Pagination,
  ProgressBar,
  RelativeTime,
  type BadgeTone,
  type DataTableColumn,
} from "@/shared/ui";
import { usePrewarmItems, type PrewarmJob } from "./queries";

/** Progress, results and actions of one prewarm job. */
const props = defineProps<{
  vendor: string;
  app: string;
  job: PrewarmJob;
  busy?: boolean;
  readonly?: boolean;
}>();
const emit = defineEmits<{ cancel: []; retry: []; dismiss: [] }>();
const i18n = useI18n();
const { t } = i18n;
const format = useFormat();
const page = ref(1);
const running = computed(() => props.job.state === "running");
const items = usePrewarmItems(
  () => props.vendor,
  () => props.app,
  computed(() => props.job.id),
  page,
  running,
);
watch(
  () => props.job.id,
  () => {
    page.value = 1;
  },
);
// Load the final results once the job stops.
watch(running, (now, before) => {
  if (before && !now) void items.refetch();
});

const stateTones: Record<PrewarmJob["state"], BadgeTone> = {
  running: "info",
  completed: "success",
  completed_with_errors: "warning",
  limited: "warning",
  cancelled: "neutral",
  interrupted: "warning",
};
const itemTones: Record<Schema<"PrewarmItem">["status"], BadgeTone> = {
  pending: "neutral",
  downloaded: "success",
  cached: "success",
  not_modified: "success",
  ttl_fallback: "warning",
  stale_fallback: "warning",
  not_cacheable: "warning",
  skipped: "neutral",
  failed: "danger",
};
const planned = computed(() => items.data.value?.total ?? null);
const ignored = computed(() => Object.entries(props.job.ignored).filter(([, count]) => count > 0));
const retryable = computed(() => !running.value && props.job.state !== "completed");
const columns = computed<DataTableColumn[]>(() => [
  { key: "key", label: t("prewarm.job.columns.file") },
  { key: "status", label: t("prewarm.job.columns.status") },
  { key: "bytes", label: t("prewarm.job.columns.size"), align: "end" },
]);

function label(prefix: string, value: string): string {
  const key = `${prefix}.${value}`;
  return i18n.te(key) ? t(key) : value;
}
</script>

<template>
  <section
    class="flex flex-col gap-4 rounded-lg border border-border p-4"
    :aria-label="t('prewarm.job.title')"
  >
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div class="flex flex-col gap-1">
        <div class="flex flex-wrap items-center gap-2">
          <h3 class="text-sm font-semibold">{{ t("prewarm.job.title") }}</h3>
          <Badge :tone="stateTones[job.state]" aria-live="polite">
            {{ t(`prewarm.job.states.${job.state}`) }}
          </Badge>
          <Badge v-if="job.automatic">{{ t("prewarm.job.automatic") }}</Badge>
        </div>
        <p class="text-xs text-muted">
          {{ t("prewarm.job.started") }} <RelativeTime :value="job.created_at" /> ·
          {{ t("prewarm.job.updated") }} <RelativeTime :value="job.updated_at" />
        </p>
        <p v-if="job.target" class="text-sm">
          {{
            job.resolved_version
              ? t("prewarm.job.targetResolved", {
                  target: job.target,
                  version: job.resolved_version,
                })
              : t("prewarm.job.target", { target: job.target })
          }}
          <template v-if="job.platforms.length"> · {{ job.platforms.join(", ") }}</template>
        </p>
        <p v-if="job.reason" class="text-sm">
          {{ label("prewarm.job.reasons", job.reason) }}
        </p>
      </div>
      <div v-if="!readonly" class="flex flex-wrap gap-2">
        <Button v-if="running" :loading="busy" @click="emit('cancel')">
          {{ t("prewarm.job.cancel") }}
        </Button>
        <template v-else>
          <Button v-if="retryable" :loading="busy" @click="emit('retry')">
            {{ t("prewarm.job.retry") }}
          </Button>
          <Button variant="ghost" :disabled="busy" @click="emit('dismiss')">
            {{ t("prewarm.job.dismiss") }}
          </Button>
        </template>
      </div>
    </div>

    <div class="flex flex-col gap-1">
      <ProgressBar
        :value="running && planned === null ? null : job.completed"
        :max="Math.max(planned ?? job.completed, 1)"
        :label="t('prewarm.job.progress')"
      />
      <p class="text-sm tabular-nums">
        {{
          t("prewarm.job.summary", {
            completed: format.number(job.completed),
            planned: planned === null ? "…" : format.number(planned),
            succeeded: format.number(job.succeeded),
            size: format.bytes(job.bytes),
          })
        }}
      </p>
      <p v-if="ignored.length" class="text-xs text-muted">
        {{ t("prewarm.job.ignored") }}
        {{ ignored.map(([why, count]) => `${why}: ${String(count)}`).join(" · ") }}
      </p>
    </div>

    <DataTable
      :columns="columns"
      :rows="items.data.value?.items"
      :row-key="(row) => row.key"
      :loading="items.isPending.value"
      :error="items.error.value"
      :caption="t('prewarm.job.items')"
      :empty-text="t('prewarm.job.noItems')"
      @retry="items.refetch()"
    >
      <template #cell-key="{ row }">
        <span class="font-mono text-xs break-all">{{ row.key }}</span>
      </template>
      <template #cell-status="{ row }">
        <div class="flex flex-col items-start gap-1">
          <Badge :tone="itemTones[row.status]">{{
            t(`prewarm.job.itemStates.${row.status}`)
          }}</Badge>
          <span v-if="row.reason" class="text-xs text-muted">{{ row.reason }}</span>
        </div>
      </template>
      <template #cell-bytes="{ row }">
        <span class="tabular-nums">{{ format.bytes(row.bytes) }}</span>
      </template>
    </DataTable>
    <Pagination
      v-if="(items.data.value?.total_pages ?? 1) > 1"
      v-model:page="page"
      :total="items.data.value?.total ?? 0"
      :page-size="items.data.value?.limit ?? 25"
    />
  </section>
</template>
