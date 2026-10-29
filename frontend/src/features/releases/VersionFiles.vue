<script setup lang="ts">
import { computed, watch } from "vue";
import { ChevronRight } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import type { Schema } from "@/shared/api";
import { useFormat } from "@/shared/i18n";
import { useCursorPagination } from "@/shared/lib";
import {
  Badge,
  Card,
  CursorPagination,
  DataTable,
  type BadgeTone,
  type DataTableColumn,
} from "@/shared/ui";
import { useResources } from "./queries";

type Resource = Schema<"Resource">;

/** Stored generations of the files of one version. */
const props = defineProps<{ vendor: string; app: string; version: string; autoRefresh: boolean }>();
const { t } = useI18n();
const format = useFormat();
const interval = computed(() => (props.autoRefresh ? 5_000 : false));

const pages = useCursorPagination();
const resources = useResources(
  () => props.vendor,
  () => props.app,
  () => props.version,
  pages.cursor,
  interval,
);
watch(
  () => [props.vendor, props.app, props.version],
  () => {
    pages.reset();
  },
);

const columns = computed<DataTableColumn[]>(() => [
  { key: "artifact", label: t("releases.resources.artifact") },
  { key: "state", label: t("releases.resources.state") },
  { key: "size", label: t("releases.resources.size"), align: "end" },
  { key: "transfer", label: t("releases.resources.transfer"), align: "end" },
  { key: "timing", label: t("releases.resources.timing") },
]);

const stateTones: Record<Resource["state"], BadgeTone> = {
  queued: "neutral",
  downloading: "info",
  resuming: "info",
  retry_wait: "warning",
  verifying: "info",
  complete: "success",
  failed: "danger",
  invalid: "danger",
  interrupted: "warning",
};

function expected(resource: Resource): number | null {
  return resource.expected_bytes ?? resource.total_bytes;
}
</script>

<template>
  <Card
    :title="t('releases.resources.title', { version })"
    :description="t('releases.resources.description')"
  >
    <div class="flex flex-col gap-3">
      <DataTable
        :columns="columns"
        :rows="resources.data.value?.items"
        :row-key="(row) => row.id"
        :loading="resources.isPending.value"
        :error="resources.error.value"
        :caption="t('releases.resources.title', { version })"
        :empty-text="t('releases.resources.empty')"
        @retry="resources.refetch()"
      >
        <template #cell-artifact="{ row }">
          <span class="font-mono break-all">{{ row.key }}</span>
        </template>
        <template #cell-state="{ row }">
          <div class="flex flex-col items-start gap-1">
            <Badge :tone="stateTones[row.state]" class="whitespace-nowrap">
              {{ t(`releases.resources.states.${row.state}`) }}
            </Badge>
            <span class="text-xs whitespace-nowrap text-muted">
              <span class="font-mono" :title="row.id">{{ row.id.slice(0, 8) }}</span>
              <template v-if="row.retired"> · {{ t("releases.resources.retired") }}</template>
              <template v-else-if="row.current"> · {{ t("releases.resources.current") }}</template>
            </span>
          </div>
        </template>
        <template #cell-size="{ row }">
          <div class="flex flex-col items-end whitespace-nowrap tabular-nums">
            <span>{{ format.bytes(row.bytes) }}</span>
            <span v-if="expected(row) !== null" class="text-xs text-muted">
              {{ t("releases.resources.of", { total: format.bytes(expected(row)) }) }}
            </span>
          </div>
        </template>
        <template #cell-transfer="{ row }">
          <div class="flex flex-col items-end text-xs whitespace-nowrap tabular-nums">
            <span>
              {{ t("releases.resources.average", { speed: format.bytes(row.average_bps) }) }}
            </span>
            <span>
              {{ t("releases.resources.recent", { speed: format.bytes(row.recent_bps) }) }}
            </span>
            <span class="text-muted">
              {{
                t("releases.resources.activity", {
                  readers: row.readers,
                  resumes: row.resumes,
                })
              }}
              <template v-if="row.active_writer"> · {{ t("releases.resources.writing") }}</template>
            </span>
          </div>
        </template>
        <template #cell-timing="{ row }">
          <div class="flex flex-col text-xs">
            <span class="whitespace-nowrap">{{ format.dateTime(row.started_at) }}</span>
            <span class="whitespace-nowrap text-muted">
              {{
                row.finished_at
                  ? format.dateTime(row.finished_at)
                  : t("releases.resources.unfinished")
              }}
            </span>
            <details v-if="row.error" class="group mt-1">
              <summary class="inline-flex items-center gap-1 rounded-sm text-danger focus-ring">
                <ChevronRight
                  class="size-3 transition-transform group-open:rotate-90"
                  aria-hidden="true"
                />
                {{ t("releases.resources.lastError") }}
              </summary>
              <p class="mt-1 font-mono break-words">{{ row.error }}</p>
            </details>
          </div>
        </template>
      </DataTable>
      <p class="text-xs text-muted">{{ t("releases.resources.speedHint") }}</p>
      <CursorPagination
        :page="pages.page.value"
        :has-previous="pages.hasPrevious.value"
        :has-next="Boolean(resources.data.value?.next_cursor)"
        :loading="resources.isFetching.value"
        @previous="pages.previous()"
        @next="pages.next(resources.data.value?.next_cursor)"
      />
    </div>
  </Card>
</template>
