<script setup lang="ts">
import { computed, watch } from "vue";
import { ChevronRight } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import { versionRoute } from "@/features/directory";
import { useFormat } from "@/shared/i18n";
import { useCursorPagination } from "@/shared/lib";
import {
  Button,
  Card,
  CursorPagination,
  DataTable,
  RelativeTime,
  type DataTableColumn,
} from "@/shared/ui";
import { useVersions } from "./queries";
import VersionFilesSummary from "./VersionFilesSummary.vue";

/** Versions of the current source epoch; each links to its stored files. */
const props = defineProps<{ vendor: string; app: string; autoRefresh: boolean }>();
const { t } = useI18n();
const format = useFormat();
const interval = computed(() => (props.autoRefresh ? 5_000 : false));

const pages = useCursorPagination();
const versions = useVersions(
  () => props.vendor,
  () => props.app,
  pages.cursor,
  interval,
);
watch(
  () => [props.vendor, props.app],
  () => {
    pages.reset();
  },
);

const columns = computed<DataTableColumn[]>(() => [
  { key: "version", label: t("releases.versions.version") },
  { key: "files", label: t("releases.versions.files") },
  { key: "first_seen", label: t("releases.versions.firstSeen") },
  { key: "requests", label: t("releases.versions.requests"), align: "end" },
  { key: "downstream_bytes", label: t("releases.versions.downstream"), align: "end" },
  { key: "actions", label: t("releases.versions.actions"), align: "end", hideLabel: true },
]);
</script>

<template>
  <Card :title="t('releases.versions.title')" :description="t('releases.versions.description')">
    <div class="flex flex-col gap-3">
      <DataTable
        :columns="columns"
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
        <template #cell-files="{ row }">
          <VersionFilesSummary :files="row.files" />
        </template>
        <template #cell-first_seen="{ row }">
          <RelativeTime :value="row.first_seen" class="whitespace-nowrap" />
        </template>
        <template #cell-requests="{ row }">
          <span class="tabular-nums">{{ format.number(row.requests) }}</span>
        </template>
        <template #cell-downstream_bytes="{ row }">
          <span class="whitespace-nowrap tabular-nums">{{
            format.bytes(row.downstream_bytes)
          }}</span>
        </template>
        <template #cell-actions="{ row }">
          <Button size="sm" variant="ghost" as-child>
            <RouterLink
              :to="versionRoute(vendor, app, row.version)"
              :aria-label="t('releases.versions.showOf', { version: row.version })"
            >
              {{ t("releases.versions.show") }}
              <ChevronRight aria-hidden="true" />
            </RouterLink>
          </Button>
        </template>
      </DataTable>
      <CursorPagination
        :page="pages.page.value"
        :has-previous="pages.hasPrevious.value"
        :has-next="Boolean(versions.data.value?.next_cursor)"
        :loading="versions.isFetching.value"
        @previous="pages.previous()"
        @next="pages.next(versions.data.value?.next_cursor)"
      />
    </div>
  </Card>
</template>
