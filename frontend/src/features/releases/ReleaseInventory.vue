<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { ChevronRight, Filter, X } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import type { Schema } from "@/shared/api";
import { useFormat } from "@/shared/i18n";
import { useCursorPagination } from "@/shared/lib";
import {
  Badge,
  Button,
  Card,
  CursorPagination,
  DataTable,
  RelativeTime,
  type BadgeTone,
  type DataTableColumn,
} from "@/shared/ui";
import { useResources, useVersions } from "./queries";

type Resource = Schema<"Resource">;

/** Versions of the current source epoch and the stored generations of their files. */
const props = defineProps<{ vendor: string; app: string; autoRefresh: boolean }>();
const { t } = useI18n();
const format = useFormat();
const interval = computed(() => (props.autoRefresh ? 5_000 : false));

const versionPages = useCursorPagination();
const resourcePages = useCursorPagination();
const version = ref<string | null>(null);
const versions = useVersions(
  () => props.vendor,
  () => props.app,
  versionPages.cursor,
  interval,
);
const resources = useResources(
  () => props.vendor,
  () => props.app,
  version,
  resourcePages.cursor,
  interval,
);
watch(version, () => {
  resourcePages.reset();
});
watch(
  () => [props.vendor, props.app],
  () => {
    version.value = null;
    versionPages.reset();
    resourcePages.reset();
  },
);

const versionColumns = computed<DataTableColumn[]>(() => [
  { key: "version", label: t("releases.versions.version") },
  { key: "first_seen", label: t("releases.versions.firstSeen") },
  { key: "requests", label: t("releases.versions.requests"), align: "end" },
  { key: "downstream_bytes", label: t("releases.versions.downstream"), align: "end" },
  { key: "actions", label: t("releases.versions.actions"), align: "end", hideLabel: true },
]);
const resourceColumns = computed<DataTableColumn[]>(() => [
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

function toggle(next: string) {
  version.value = version.value === next ? null : next;
}

function expected(resource: Resource): number | null {
  return resource.expected_bytes ?? resource.total_bytes;
}
</script>

<template>
  <div class="grid gap-6 xl:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]">
    <Card :title="t('releases.versions.title')" :description="t('releases.versions.description')">
      <div class="flex flex-col gap-3">
        <DataTable
          :columns="versionColumns"
          :rows="versions.data.value?.items"
          :row-key="(row) => row.version"
          :loading="versions.isPending.value"
          :error="versions.error.value"
          :caption="t('releases.versions.title')"
          :empty-text="t('releases.versions.empty')"
          @retry="versions.refetch()"
        >
          <template #cell-version="{ row }">
            <span class="font-mono font-medium">{{ row.version }}</span>
          </template>
          <template #cell-first_seen="{ row }">
            <RelativeTime :value="row.first_seen" />
          </template>
          <template #cell-requests="{ row }">
            <span class="tabular-nums">{{ format.number(row.requests) }}</span>
          </template>
          <template #cell-downstream_bytes="{ row }">
            <span class="tabular-nums">{{ format.bytes(row.downstream_bytes) }}</span>
          </template>
          <template #cell-actions="{ row }">
            <Button
              size="sm"
              variant="ghost"
              :aria-pressed="version === row.version"
              :aria-label="t('releases.versions.filter', { version: row.version })"
              @click="toggle(row.version)"
            >
              <Filter aria-hidden="true" />
              {{ t("releases.versions.files") }}
            </Button>
          </template>
        </DataTable>
        <CursorPagination
          :page="versionPages.page.value"
          :has-previous="versionPages.hasPrevious.value"
          :has-next="Boolean(versions.data.value?.next_cursor)"
          :loading="versions.isFetching.value"
          @previous="versionPages.previous()"
          @next="versionPages.next(versions.data.value?.next_cursor)"
        />
      </div>
    </Card>

    <Card :title="t('releases.resources.title')" :description="t('releases.resources.description')">
      <template #actions>
        <span v-if="version" class="inline-flex items-center gap-1">
          <Badge tone="primary">{{ t("releases.resources.only", { version }) }}</Badge>
          <Button
            size="sm"
            variant="ghost"
            icon
            :aria-label="t('releases.resources.clearFilter')"
            @click="version = null"
          >
            <X aria-hidden="true" />
          </Button>
        </span>
      </template>
      <div class="flex flex-col gap-3">
        <DataTable
          :columns="resourceColumns"
          :rows="resources.data.value?.items"
          :row-key="(row) => row.id"
          :loading="resources.isPending.value"
          :error="resources.error.value"
          :caption="t('releases.resources.title')"
          :empty-text="t('releases.resources.empty')"
          @retry="resources.refetch()"
        >
          <template #cell-artifact="{ row }">
            <div class="flex flex-col">
              <span class="font-mono font-medium">{{ row.version }}</span>
              <span class="font-mono text-xs break-all text-muted">{{ row.key }}</span>
            </div>
          </template>
          <template #cell-state="{ row }">
            <div class="flex flex-col items-start gap-1">
              <Badge :tone="stateTones[row.state]">
                {{ t(`releases.resources.states.${row.state}`) }}
              </Badge>
              <span class="text-xs text-muted">
                <span class="font-mono" :title="row.id">{{ row.id.slice(0, 8) }}</span>
                <template v-if="row.retired"> · {{ t("releases.resources.retired") }}</template>
                <template v-else-if="row.current">
                  · {{ t("releases.resources.current") }}</template
                >
              </span>
            </div>
          </template>
          <template #cell-size="{ row }">
            <div class="flex flex-col items-end tabular-nums">
              <span>{{ format.bytes(row.bytes) }}</span>
              <span v-if="expected(row) !== null" class="text-xs text-muted">
                {{ t("releases.resources.of", { total: format.bytes(expected(row)) }) }}
              </span>
            </div>
          </template>
          <template #cell-transfer="{ row }">
            <div class="flex flex-col items-end text-xs tabular-nums">
              <span>
                {{
                  t("releases.resources.speed", {
                    average: format.bytes(row.average_bps),
                    recent: format.bytes(row.recent_bps),
                  })
                }}
              </span>
              <span class="text-muted">
                {{
                  t("releases.resources.activity", {
                    readers: row.readers,
                    resumes: row.resumes,
                  })
                }}
                <template v-if="row.active_writer">
                  · {{ t("releases.resources.writing") }}</template
                >
              </span>
            </div>
          </template>
          <template #cell-timing="{ row }">
            <div class="flex flex-col text-xs">
              <span>{{ format.dateTime(row.started_at) }}</span>
              <span class="text-muted">
                {{
                  row.finished_at
                    ? format.dateTime(row.finished_at)
                    : t("releases.resources.unfinished")
                }}
              </span>
              <details v-if="row.error" class="group mt-1">
                <summary
                  class="inline-flex cursor-pointer items-center gap-1 rounded-sm text-danger focus-ring"
                >
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
          :page="resourcePages.page.value"
          :has-previous="resourcePages.hasPrevious.value"
          :has-next="Boolean(resources.data.value?.next_cursor)"
          :loading="resources.isFetching.value"
          @previous="resourcePages.previous()"
          @next="resourcePages.next(resources.data.value?.next_cursor)"
        />
      </div>
    </Card>
  </div>
</template>
