<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import { useFormat } from "@/shared/i18n";
import { Badge, DataTable, type DataTableColumn } from "@/shared/ui";
import type { OperationalEvent } from "./queries";

/** Operational events, newest first. Messages are shown as reported by the server (English). */
defineProps<{
  events: OperationalEvent[] | undefined;
  loading?: boolean;
  error?: unknown;
}>();
const emit = defineEmits<{ retry: [] }>();
const { t } = useI18n();
const format = useFormat();

const columns = computed<DataTableColumn[]>(() => [
  { key: "time", label: t("events.columns.time"), class: "whitespace-nowrap" },
  { key: "event", label: t("events.columns.event") },
  { key: "app", label: t("events.columns.app") },
  { key: "resource", label: t("events.columns.resource") },
  { key: "message", label: t("events.columns.message"), class: "min-w-64" },
]);

/** The application page; it opens the provider's default tab. */
function appRoute(key: string): string {
  const [vendor = "", app = ""] = key.split("/");
  return `/admin/vendors/${encodeURIComponent(vendor)}/apps/${encodeURIComponent(app)}`;
}
</script>

<template>
  <DataTable
    :columns="columns"
    :rows="events"
    :row-key="(event) => String(event.id)"
    :caption="t('events.caption')"
    :loading="loading"
    :error="error"
    :empty-text="t('events.empty')"
    @retry="emit('retry')"
  >
    <template #cell-time="{ row }">
      <time :datetime="row.time" class="tabular-nums">
        {{ format.dateTime(row.time, { dateStyle: "medium", timeStyle: "medium" }) }}
      </time>
    </template>
    <template #cell-event="{ row }">
      <div class="flex flex-col items-start gap-1">
        <span class="flex flex-wrap items-center gap-1.5">
          <Badge>{{ row.category }}</Badge>
          <Badge v-if="row.status_code !== null" tone="danger">
            {{ t("events.httpStatus", { status: row.status_code }) }}
          </Badge>
        </span>
        <code class="text-xs text-muted">{{ row.code }}</code>
      </div>
    </template>
    <template #cell-app="{ row }">
      <div v-if="row.app_key" class="flex flex-col gap-0.5">
        <RouterLink
          :to="appRoute(row.app_key)"
          class="rounded-sm font-mono text-xs text-primary hover:underline focus-ring"
        >
          {{ row.app_key }}
        </RouterLink>
        <span v-if="row.version" class="text-xs text-muted">
          {{ t("events.version", { version: row.version }) }}
        </span>
      </div>
      <span v-else class="text-muted">—</span>
    </template>
    <template #cell-resource="{ row }">
      <div v-if="row.resource_key" class="flex flex-col gap-0.5">
        <code class="font-mono text-xs break-all">{{ row.resource_key }}</code>
        <span v-if="row.generation_id" class="text-xs text-muted">
          {{ t("events.generation", { id: row.generation_id }) }}
        </span>
      </div>
      <span v-else class="text-muted">—</span>
    </template>
    <template #cell-message="{ row }">
      <span lang="en" class="break-words">{{ row.message }}</span>
    </template>
  </DataTable>
</template>
