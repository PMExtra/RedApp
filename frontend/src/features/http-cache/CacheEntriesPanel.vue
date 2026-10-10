<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { ChevronRight, RefreshCw } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { useFormat } from "@/shared/i18n";
import { toast, useCursorPagination } from "@/shared/lib";
import {
  Badge,
  Card,
  CursorPagination,
  DataTable,
  IconButton,
  type DataTableColumn,
} from "@/shared/ui";
import { useCacheEntries, useRefreshEntry, type CacheEntry } from "./queries";

/** Cached files of the selected source epoch, with per-file refresh. */
const props = defineProps<{
  vendor: string;
  app: string;
  /** `null` for the current source epoch. */
  sourceEpoch: number | null;
  /** Refresh works only on the current, active source of an enabled application. */
  canRefresh: boolean;
}>();
const { t } = useI18n();
const format = useFormat();
const pages = useCursorPagination();
const epoch = computed(() => props.sourceEpoch);
const entries = useCacheEntries(
  () => props.vendor,
  () => props.app,
  epoch,
  pages.cursor,
);
watch([epoch, () => props.vendor, () => props.app], () => {
  pages.reset();
});
const refresh = useRefreshEntry(
  () => props.vendor,
  () => props.app,
);
const refreshing = ref<string | null>(null);

const columns = computed<DataTableColumn[]>(() => [
  { key: "path", label: t("httpCache.entries.path") },
  { key: "size_bytes", label: t("httpCache.entries.size"), align: "end" },
  { key: "fetched_at", label: t("httpCache.entries.fetched") },
  { key: "last_access_at", label: t("httpCache.entries.accessed") },
  { key: "fresh_until", label: t("httpCache.entries.freshness") },
  { key: "actions", label: t("httpCache.entries.actions"), hideLabel: true, align: "end" },
]);

function fresh(entry: CacheEntry): boolean {
  return new Date(entry.fresh_until).getTime() > Date.now();
}

function refreshFile(entry: CacheEntry) {
  refreshing.value = entry.path;
  refresh.mutate(entry.path, {
    onSuccess: (result) => {
      toast({
        tone: result.status === "failed" ? "warning" : "success",
        title: t(`httpCache.entries.results.${result.status}`, { path: result.path }),
        description: result.reason,
      });
    },
    onSettled: () => {
      refreshing.value = null;
    },
  });
}
</script>

<template>
  <Card :title="t('httpCache.entries.title')" :description="t('httpCache.entries.description')">
    <div class="flex flex-col gap-3">
      <DataTable
        :columns="columns"
        :rows="entries.data.value?.items"
        :row-key="(row) => row.generation_id"
        :loading="entries.isPending.value"
        :error="entries.error.value"
        :caption="t('httpCache.entries.title')"
        :empty-text="t('httpCache.entries.empty')"
        @retry="entries.refetch()"
      >
        <template #cell-path="{ row }">
          <details class="group">
            <summary
              class="inline-flex cursor-pointer items-start gap-1 rounded-sm font-mono text-xs break-all focus-ring"
            >
              <ChevronRight
                class="mt-0.5 size-3 shrink-0 transition-transform group-open:rotate-90"
                aria-hidden="true"
              />
              {{ row.path }}
            </summary>
            <dl class="mt-2 grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-xs">
              <dt class="text-muted">{{ t("httpCache.entries.source") }}</dt>
              <dd class="font-mono break-all">{{ row.source_url }}</dd>
              <dt class="text-muted">{{ t("httpCache.entries.validated") }}</dt>
              <dd>{{ format.dateTime(row.validated_at) }}</dd>
              <dt class="text-muted">ETag</dt>
              <dd class="font-mono break-all">{{ row.etag ?? t("common.states.unknown") }}</dd>
              <dt class="text-muted">SHA-256</dt>
              <dd class="font-mono break-all">{{ row.sha256 }}</dd>
              <dt class="text-muted">{{ t("httpCache.entries.generation") }}</dt>
              <dd class="font-mono break-all">{{ row.generation_id }}</dd>
            </dl>
          </details>
        </template>
        <template #cell-size_bytes="{ row }">
          <span class="tabular-nums">{{ format.bytes(row.size_bytes) }}</span>
        </template>
        <template #cell-fetched_at="{ row }">
          <span class="text-xs">{{ format.dateTime(row.fetched_at) }}</span>
        </template>
        <template #cell-last_access_at="{ row }">
          <span class="text-xs">{{
            row.last_access_at ? format.dateTime(row.last_access_at) : t("httpCache.entries.never")
          }}</span>
        </template>
        <template #cell-fresh_until="{ row }">
          <div class="flex flex-col items-start gap-1">
            <Badge :tone="fresh(row) ? 'success' : 'neutral'">
              {{ fresh(row) ? t("httpCache.entries.fresh") : t("httpCache.entries.stale") }}
            </Badge>
            <span class="text-xs text-muted">{{ format.dateTime(row.fresh_until) }}</span>
          </div>
        </template>
        <template #cell-actions="{ row }">
          <IconButton
            v-if="canRefresh"
            size="sm"
            :label="t('httpCache.entries.refresh', { path: row.path })"
            :loading="refreshing === row.path"
            :disabled="refreshing !== null && refreshing !== row.path"
            @click="refreshFile(row)"
          >
            <RefreshCw aria-hidden="true" />
          </IconButton>
        </template>
      </DataTable>
      <p class="text-xs text-muted">{{ t("httpCache.entries.hint") }}</p>
      <CursorPagination
        :page="pages.page.value"
        :has-previous="pages.hasPrevious.value"
        :has-next="Boolean(entries.data.value?.next_cursor)"
        :loading="entries.isFetching.value"
        @previous="pages.previous()"
        @next="pages.next(entries.data.value?.next_cursor)"
      />
    </div>
  </Card>
</template>
