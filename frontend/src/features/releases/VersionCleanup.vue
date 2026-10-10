<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { isApiError } from "@/shared/api";
import { useFormat } from "@/shared/i18n";
import { confirm, toast } from "@/shared/lib";
import {
  Alert,
  Button,
  Card,
  DataTable,
  Field,
  Input,
  Pagination,
  type DataTableColumn,
} from "@/shared/ui";
import { useExpired } from "./expiry";
import SourceEpochSelect from "./SourceEpochSelect.vue";
import { useVersionCleanup, type VersionCleanupPreview } from "./queries";

/**
 * Removes cached generations below a minimum version: a frozen preview
 * (valid 10 minutes), reviewed and then executed by its ID.
 */
const props = defineProps<{ vendor: string; app: string; disabled?: boolean }>();
const { t } = useI18n();
const format = useFormat();
const PAGE_SIZE = 25;

const sourceEpoch = ref<number | null>(null);
const minimum = ref("");
const preview = ref<VersionCleanupPreview>();
const stale = ref(false);
const page = ref(1);
const { preview: create, execute } = useVersionCleanup(
  () => props.vendor,
  () => props.app,
);
const expired = useExpired(() =>
  preview.value && !preview.value.executed_at ? preview.value.expires_at : undefined,
);
const busy = computed(() => create.isPending.value || execute.isPending.value);

// The preview belongs to the inputs it was made from.
watch([sourceEpoch, minimum, () => props.vendor, () => props.app], () => {
  preview.value = undefined;
  stale.value = false;
});

const rows = computed(() => {
  const start = (page.value - 1) * PAGE_SIZE;
  return (preview.value?.selected ?? []).slice(start, start + PAGE_SIZE);
});
const columns = computed<DataTableColumn[]>(() => [
  { key: "version", label: t("releases.cleanup.columns.version") },
  { key: "resource_key", label: t("releases.cleanup.columns.file") },
  { key: "generation_id", label: t("releases.cleanup.columns.generation") },
  { key: "bytes", label: t("releases.cleanup.columns.size"), align: "end" },
]);

async function submit() {
  const version = minimum.value.trim();
  if (!version || busy.value) return;
  stale.value = false;
  page.value = 1;
  preview.value = await create.mutateAsync({
    minimum_version: version,
    ...(sourceEpoch.value === null ? {} : { source_epoch: sourceEpoch.value }),
  });
}

async function run() {
  const current = preview.value;
  if (!current || expired.value || busy.value) return;
  const confirmed = await confirm({
    title: t("releases.cleanup.confirmTitle"),
    description: t("releases.cleanup.confirmDescription", {
      count: current.selected.length,
      size: format.bytes(current.logical_bytes),
    }),
    confirmLabel: t("releases.cleanup.execute"),
    tone: "danger",
  });
  if (!confirmed) return;
  try {
    preview.value = await execute.mutateAsync(current.id);
    toast({ tone: "success", title: t("releases.cleanup.done") });
  } catch (error) {
    if (isApiError(error, "PREVIEW_NOT_FOUND", "PREVIEW_STALE")) {
      preview.value = undefined;
      stale.value = true;
    }
  }
}

function cancel() {
  preview.value = undefined;
}
</script>

<template>
  <Card :title="t('releases.cleanup.title')" :description="t('releases.cleanup.description')">
    <div class="flex flex-col gap-4">
      <form class="flex flex-col gap-4" @submit.prevent="submit().catch(() => undefined)">
        <SourceEpochSelect
          v-model="sourceEpoch"
          :vendor="vendor"
          :app="app"
          :disabled="disabled || busy"
        />
        <div class="flex flex-wrap items-start gap-3">
          <Field
            v-slot="{ control }"
            class="w-64"
            :label="t('releases.cleanup.minimum')"
            :description="t('releases.cleanup.minimumHint')"
            required
          >
            <Input
              v-bind="control"
              v-model="minimum"
              maxlength="128"
              autocomplete="off"
              spellcheck="false"
              placeholder="1.0.0"
              :disabled="disabled || busy"
            />
          </Field>
          <Button
            type="submit"
            class="mt-6.5"
            :loading="create.isPending.value"
            :disabled="disabled || !minimum.trim() || execute.isPending.value"
          >
            {{ t("releases.cleanup.preview") }}
          </Button>
        </div>
      </form>

      <Alert v-if="stale" tone="warning" :title="t('releases.preview.staleTitle')">
        {{ t("releases.preview.staleDescription") }}
      </Alert>

      <section
        v-if="preview"
        class="flex flex-col gap-3 rounded-lg border border-border p-4"
        :aria-label="t('releases.cleanup.review')"
      >
        <h3 class="text-sm font-semibold">
          {{ preview.executed_at ? t("releases.cleanup.result") : t("releases.cleanup.review") }}
        </h3>
        <dl class="grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
          <div>
            <dt class="text-xs text-muted">{{ t("releases.cleanup.generations") }}</dt>
            <dd class="font-medium tabular-nums">{{ format.number(preview.selected.length) }}</dd>
          </div>
          <div>
            <dt class="text-xs text-muted">{{ t("releases.cleanup.logical") }}</dt>
            <dd class="font-medium tabular-nums">{{ format.bytes(preview.logical_bytes) }}</dd>
          </div>
          <div>
            <dt class="text-xs text-muted">{{ t("releases.cleanup.reclaimable") }}</dt>
            <dd class="font-medium tabular-nums">{{ format.bytes(preview.reclaimable_bytes) }}</dd>
          </div>
          <div>
            <dt class="text-xs text-muted">{{ t("releases.cleanup.active") }}</dt>
            <dd class="font-medium tabular-nums">
              {{ format.number(preview.active_generations) }}
            </dd>
          </div>
        </dl>
        <p v-if="preview.unknown_versions.length" class="text-sm">
          {{ t("releases.cleanup.unknown", { versions: preview.unknown_versions.join(", ") }) }}
        </p>
        <DataTable
          :columns="columns"
          :rows="rows"
          :row-key="(row) => row.generation_id"
          :caption="t('releases.cleanup.selected')"
          :empty-text="t('releases.cleanup.nothing')"
        >
          <template #cell-version="{ row }">
            <span class="font-mono">{{ row.version }}</span>
          </template>
          <template #cell-resource_key="{ row }">
            <span class="font-mono text-xs break-all">{{ row.resource_key }}</span>
          </template>
          <template #cell-generation_id="{ row }">
            <span class="font-mono text-xs" :title="row.generation_id">
              {{ row.generation_id.slice(0, 8) }}
            </span>
          </template>
          <template #cell-bytes="{ row }">
            <span class="tabular-nums">{{ format.bytes(row.bytes) }}</span>
          </template>
        </DataTable>
        <Pagination
          v-if="preview.selected.length > PAGE_SIZE"
          v-model:page="page"
          :total="preview.selected.length"
          :page-size="PAGE_SIZE"
        />
        <template v-if="preview.executed_at">
          <Alert tone="success">{{ t("releases.cleanup.executed") }}</Alert>
        </template>
        <template v-else>
          <p class="text-xs text-muted">
            {{ t("releases.preview.expires", { time: format.dateTime(preview.expires_at) }) }}
          </p>
          <Alert v-if="expired" tone="warning" :title="t('releases.preview.expiredTitle')">
            {{ t("releases.preview.expiredDescription") }}
          </Alert>
          <p class="text-xs text-muted">{{ t("releases.cleanup.drain") }}</p>
          <div class="flex flex-wrap gap-2">
            <Button
              variant="danger"
              :loading="execute.isPending.value"
              :disabled="disabled || expired || preview.selected.length === 0"
              @click="run"
            >
              {{ t("releases.cleanup.execute") }}
            </Button>
            <Button :disabled="execute.isPending.value" @click="cancel">
              {{ t("common.actions.cancel") }}
            </Button>
          </div>
        </template>
      </section>
    </div>
  </Card>
</template>
