<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useAppConfiguration } from "@/features/configuration";
import { useExpired } from "@/features/releases";
import { isApiError } from "@/shared/api";
import { useFormat } from "@/shared/i18n";
import { confirm, toast } from "@/shared/lib";
import {
  Alert,
  Badge,
  Button,
  DataTable,
  Pagination,
  RevisionConflictAlert,
  type DataTableColumn,
} from "@/shared/ui";
import { useRetentionActions, useRetentionItems, useRetentionPreview } from "./queries";

/**
 * A manual retention run with the saved policy: preview → review → execute.
 * `dirty` (unsaved policy changes) blocks previews until they are saved.
 */
const props = defineProps<{
  vendor: string;
  app: string;
  readOnly?: boolean;
  /** The application or vendor is disabled: manual runs are refused. */
  inactive?: boolean;
  dirty?: boolean;
}>();
const i18n = useI18n();
const { t } = i18n;
const format = useFormat();

const configuration = useAppConfiguration(
  () => props.vendor,
  () => props.app,
);
const previewId = ref<string | null>(null);
const page = ref(1);
const stale = ref(false);
const actions = useRetentionActions(
  () => props.vendor,
  () => props.app,
  () => configuration.data.value?.revision,
);
const preview = useRetentionPreview(
  () => props.vendor,
  () => props.app,
  previewId,
);
const items = useRetentionItems(
  () => props.vendor,
  () => props.app,
  previewId,
  page,
);
const current = computed(() => (previewId.value ? preview.data.value : undefined));
const expired = useExpired(() =>
  current.value && !current.value.executed_at ? current.value.expires_at : undefined,
);
watch(
  () => preview.error.value,
  (error) => {
    if (isApiError(error, "PREVIEW_NOT_FOUND")) discard(true);
  },
);
watch([() => props.vendor, () => props.app], () => {
  discard(false);
  actions.preview.dismissConflict();
});

function discard(wasStale: boolean) {
  previewId.value = null;
  page.value = 1;
  stale.value = wasStale;
}

async function createPreview() {
  if (!configuration.data.value || props.dirty) return;
  stale.value = false;
  try {
    const result = await actions.preview.mutateAsync();
    page.value = 1;
    previewId.value = result.id;
  } catch {
    // A conflict shows RevisionConflictAlert; other failures are toasted globally.
  }
}

/** The policy changed since it was loaded: show the new one before previewing again. */
async function reloadPolicy() {
  actions.preview.dismissConflict();
  await configuration.refetch();
}

async function execute() {
  const value = current.value;
  if (!value || expired.value) return;
  const confirmed = await confirm({
    title: t("retention.executeTitle"),
    description: t("retention.executeDescription", {
      count: value.selected_versions,
      size: format.bytes(value.logical_bytes),
    }),
    confirmLabel: t("retention.execute"),
    tone: "danger",
  });
  if (!confirmed) return;
  try {
    await actions.execute.mutateAsync(value.id);
    toast({ tone: "success", title: t("retention.executed") });
  } catch (error) {
    if (isApiError(error, "PREVIEW_NOT_FOUND", "PREVIEW_STALE")) discard(true);
  }
}

const columns = computed<DataTableColumn[]>(() => [
  { key: "version", label: t("retention.columns.version") },
  { key: "selected", label: t("retention.columns.decision") },
  { key: "reasons", label: t("retention.columns.reasons") },
  { key: "bytes", label: t("retention.columns.size"), align: "end" },
]);

function reason(value: string): string {
  const key = `retention.reasons.${value}`;
  return i18n.te(key) ? t(key) : value;
}

const skipped = computed(() => Object.entries(current.value?.result?.skipped ?? {}));
</script>

<template>
  <section class="flex flex-col gap-3" aria-labelledby="retention-run">
    <div class="flex flex-col gap-1">
      <h3 id="retention-run" class="text-sm font-semibold">{{ t("retention.run.title") }}</h3>
      <p class="text-xs text-muted">{{ t("retention.run.hint") }}</p>
    </div>
    <div class="flex flex-wrap items-center gap-3">
      <Button
        :loading="actions.preview.isPending.value"
        :disabled="
          readOnly ||
          inactive ||
          dirty ||
          !configuration.data.value ||
          actions.execute.isPending.value
        "
        @click="createPreview"
      >
        {{ t("retention.run.preview") }}
      </Button>
      <span v-if="dirty" class="text-xs text-muted">
        {{ t("retention.run.saveFirst") }}
      </span>
    </div>
    <RevisionConflictAlert
      v-if="actions.preview.hasConflict.value"
      :reloading="configuration.isFetching.value"
      @reload="reloadPolicy"
    />
    <Alert v-if="stale" tone="warning" :title="t('releases.preview.staleTitle')">
      {{ t("releases.preview.staleDescription") }}
    </Alert>

    <div
      v-if="current"
      class="flex flex-col gap-3 rounded-lg border border-border p-4"
      role="region"
      :aria-label="t('retention.run.review')"
    >
      <dl class="grid grid-cols-1 gap-3 text-sm sm:grid-cols-3">
        <div>
          <dt class="text-xs text-muted">{{ t("retention.run.versions") }}</dt>
          <dd class="font-medium tabular-nums">
            {{ format.number(current.selected_versions) }}
          </dd>
        </div>
        <div>
          <dt class="text-xs text-muted">{{ t("retention.run.logical") }}</dt>
          <dd class="font-medium tabular-nums">{{ format.bytes(current.logical_bytes) }}</dd>
        </div>
        <div>
          <dt class="text-xs text-muted">{{ t("retention.run.reclaimable") }}</dt>
          <dd class="font-medium tabular-nums">
            {{ format.bytes(current.reclaimable_bytes) }}
          </dd>
        </div>
      </dl>
      <DataTable
        :columns="columns"
        :rows="items.data.value?.items"
        :row-key="(row) => row.version"
        :loading="items.isPending.value"
        :error="items.error.value"
        :caption="t('retention.run.items')"
        :empty-text="t('retention.run.noVersions')"
        @retry="items.refetch()"
      >
        <template #cell-version="{ row }">
          <span class="font-mono">{{ row.version }}</span>
        </template>
        <template #cell-selected="{ row }">
          <Badge :tone="row.selected ? 'danger' : 'neutral'">
            {{ row.selected ? t("retention.run.delete") : t("retention.run.keep") }}
          </Badge>
        </template>
        <template #cell-reasons="{ row }">
          {{ row.reasons.map(reason).join(", ") }}
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

      <template v-if="current.result">
        <Alert tone="success" :title="t('retention.run.receipt')">
          {{
            t("retention.run.receiptText", {
              count: current.result.retired_versions,
              size: format.bytes(current.result.logical_bytes),
            })
          }}
          <ul v-if="skipped.length" class="mt-1 list-disc ps-5">
            <li v-for="[version, why] in skipped" :key="version">
              {{ t("retention.run.skipped", { version, reason: reason(why) }) }}
            </li>
          </ul>
        </Alert>
      </template>
      <template v-else>
        <p class="text-xs text-muted">
          {{ t("releases.preview.expires", { time: format.dateTime(current.expires_at) }) }}
        </p>
        <Alert v-if="expired" tone="warning" :title="t('releases.preview.expiredTitle')">
          {{ t("releases.preview.expiredDescription") }}
        </Alert>
        <div class="flex flex-wrap gap-2">
          <Button
            variant="danger"
            :loading="actions.execute.isPending.value"
            :disabled="readOnly || inactive || expired || current.selected_versions === 0"
            @click="execute"
          >
            {{ t("retention.execute") }}
          </Button>
          <Button :disabled="actions.execute.isPending.value" @click="discard(false)">
            {{ t("common.actions.cancel") }}
          </Button>
        </div>
      </template>
    </div>
  </section>
</template>
