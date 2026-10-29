<script setup lang="ts">
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import { useFormat } from "@/shared/i18n";
import { Badge, CopyButton, DataTable, Dialog, type DataTableColumn } from "@/shared/ui";
import type { OperationalEvent } from "./queries";

/**
 * Operational events, newest first. Messages are shown as reported by the
 * server (English) on one line; a long or multi-line message opens in full
 * in a dialog. `hideApp` drops the application column on an application page.
 */
const props = defineProps<{
  events: OperationalEvent[] | undefined;
  loading?: boolean;
  error?: unknown;
  hideApp?: boolean;
}>();
const emit = defineEmits<{ retry: [] }>();
const { t } = useI18n();
const format = useFormat();

const columns = computed<DataTableColumn[]>(() => [
  { key: "time", label: t("events.columns.time"), class: "whitespace-nowrap" },
  { key: "event", label: t("events.columns.event") },
  ...(props.hideApp ? [] : [{ key: "app", label: t("events.columns.app") }]),
  { key: "resource", label: t("events.columns.resource") },
  { key: "message", label: t("events.columns.message"), class: "w-full min-w-64 max-w-0" },
]);

const selected = ref<OperationalEvent>();
const open = computed({
  get: () => selected.value !== undefined,
  set: (value: boolean) => {
    if (!value) selected.value = undefined;
  },
});

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
      <button
        type="button"
        class="block w-full truncate rounded-sm text-start hover:underline focus-ring"
        :aria-label="t('events.showMessage', { message: row.message })"
        @click="selected = row"
      >
        <span lang="en">{{ row.message }}</span>
      </button>
    </template>
  </DataTable>
  <Dialog v-model:open="open" :title="t('events.messageTitle')" size="lg">
    <div v-if="selected" class="flex flex-col gap-3">
      <p class="flex flex-wrap items-center gap-1.5 text-sm text-muted">
        <time :datetime="selected.time" class="tabular-nums">
          {{ format.dateTime(selected.time, { dateStyle: "medium", timeStyle: "medium" }) }}
        </time>
        <Badge>{{ selected.category }}</Badge>
        <code class="text-xs">{{ selected.code }}</code>
      </p>
      <pre
        lang="en"
        class="max-h-[60dvh] overflow-auto rounded-md bg-surface-sunken p-3 font-mono text-sm break-all whitespace-pre-wrap"
        >{{ selected.message }}</pre>
    </div>
    <template #footer>
      <CopyButton v-if="selected" :text="selected.message" show-label />
    </template>
  </Dialog>
</template>
