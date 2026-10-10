<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useOverlayDraft } from "@/features/cache-policy";
import {
  FieldReset,
  useAppConfiguration,
  useAppConfigurationPatch,
} from "@/features/configuration";
import { useExpired } from "@/features/releases";
import { isApiError, type Schema } from "@/shared/api";
import { useDirtyGuard } from "@/shared/forms";
import { useFormat } from "@/shared/i18n";
import { confirm, toast } from "@/shared/lib";
import {
  Alert,
  AsyncState,
  Badge,
  Button,
  Card,
  DataTable,
  Field,
  NumberInput,
  Pagination,
  RelativeTime,
  RevisionConflictAlert,
  Switch,
  type DataTableColumn,
} from "@/shared/ui";
import {
  useRetentionActions,
  useRetentionItems,
  useRetentionPreview,
  useRetentionStatus,
} from "./queries";

type Policy = Schema<"RetentionPolicy">;

/**
 * Keep-latest retention of a release application: the saved policy, the last
 * automatic run, and a manual run as preview → review → execute.
 */
const props = defineProps<{ vendor: string; app: string; readonly?: boolean }>();
const i18n = useI18n();
const { t } = i18n;
const format = useFormat();

const configuration = useAppConfiguration(
  () => props.vendor,
  () => props.app,
);
const overlay = useOverlayDraft<{ retention: Policy }>(configuration.data, (spec) =>
  spec?.retention ? { retention: spec.retention } : undefined,
);
const { draft } = overlay;
const save = useAppConfigurationPatch(
  () => props.vendor,
  () => props.app,
  configuration.data,
);
useDirtyGuard(overlay.dirty);
const status = useRetentionStatus(
  () => props.vendor,
  () => props.app,
);

const enabled = computed({
  get: () => draft.value?.retention.enabled ?? false,
  set: (value: boolean) => {
    void setEnabled(value);
  },
});
const keepLatest = computed({
  get: () => draft.value?.retention.keep_latest ?? null,
  set: (value: number | null) => {
    if (draft.value && value !== null) {
      draft.value = { retention: { ...draft.value.retention, keep_latest: value } };
    }
  },
});

async function setEnabled(value: boolean) {
  if (!draft.value) return;
  const wasEnabled = overlay.saved.value?.retention.enabled ?? false;
  if (value && !wasEnabled) {
    const confirmed = await confirm({
      title: t("retention.enableTitle"),
      description: t("retention.enableDescription"),
      confirmLabel: t("retention.enableConfirm"),
      tone: "danger",
    });
    if (!confirmed) return;
  }
  draft.value = { retention: { ...draft.value.retention, enabled: value } };
}

const keepError = computed(() => {
  const value = draft.value?.retention.keep_latest;
  return value !== undefined && Number.isInteger(value) && value >= 1 && value <= 1000
    ? undefined
    : t("retention.keepRange");
});

function submit() {
  const body = overlay.patch.value;
  if (!body || keepError.value || props.readonly) return;
  save.mutate(body, {
    onSuccess: () => {
      overlay.reset();
      toast({ tone: "success", title: t("retention.saved") });
    },
  });
}

async function reload() {
  await save.reload();
  overlay.reset();
}

// ---- manual run
const previewId = ref<string | null>(null);
const page = ref(1);
const notice = ref<"stale" | "changed" | null>(null);
const actions = useRetentionActions(
  () => props.vendor,
  () => props.app,
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
    if (isApiError(error, "PREVIEW_NOT_FOUND")) discard("stale");
  },
);
watch([() => props.vendor, () => props.app], () => {
  discard(null);
});

function discard(reason: "stale" | "changed" | null) {
  previewId.value = null;
  page.value = 1;
  notice.value = reason;
}

async function createPreview() {
  const revision = configuration.data.value?.revision;
  if (revision === undefined || overlay.dirty.value) return;
  notice.value = null;
  try {
    const result = await actions.preview.mutateAsync(revision);
    page.value = 1;
    previewId.value = result.id;
  } catch (error) {
    if (isApiError(error, "REVISION_CONFLICT")) {
      notice.value = "changed";
      void configuration.refetch();
    }
  }
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
    if (isApiError(error, "PREVIEW_NOT_FOUND", "PREVIEW_STALE")) discard("stale");
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

const lastRun = computed(() => status.data.value?.last_run ?? null);
const outcomeTone = { success: "success", failure: "danger", skip: "warning" } as const;
const skipped = computed(() => Object.entries(current.value?.result?.skipped ?? {}));
</script>

<template>
  <Card :title="t('retention.title')" :description="t('retention.description')">
    <div class="flex flex-col gap-6">
      <AsyncState
        :loading="configuration.isPending.value"
        :error="configuration.error.value"
        @retry="configuration.refetch()"
      >
        <form v-if="draft" class="flex flex-col gap-4" novalidate @submit.prevent="submit">
          <Alert v-if="configuration.data.value?.template_missing" tone="warning">
            {{ t("retention.templateMissing") }}
          </Alert>
          <RevisionConflictAlert v-if="save.hasConflict.value" @reload="reload" />
          <div class="flex items-start gap-3">
            <Switch
              id="retention-enabled"
              v-model="enabled"
              aria-describedby="retention-enabled-hint"
              :disabled="readonly || save.isPending.value"
            />
            <div class="flex min-w-0 flex-1 flex-col gap-1">
              <label for="retention-enabled" class="text-sm font-medium">
                {{ t("retention.enabled") }}
              </label>
              <p id="retention-enabled-hint" class="text-xs text-muted">
                {{ t("retention.enabledHint") }}
              </p>
            </div>
            <FieldReset
              :linked="(configuration.data.value?.template_ref ?? null) !== null"
              :origin="configuration.data.value?.fields.retention"
              :modified="overlay.modified('retention')"
              :disabled="readonly || save.isPending.value"
              @reset="overlay.restore('retention')"
            />
          </div>
          <Field
            v-slot="{ control }"
            class="w-64"
            :label="t('retention.keepLatest')"
            :description="t('retention.keepLatestHint')"
            :error="overlay.dirty.value ? keepError : undefined"
            required
          >
            <NumberInput
              v-bind="control"
              v-model="keepLatest"
              :min="1"
              :max="1000"
              :step="1"
              :disabled="readonly || save.isPending.value"
            />
          </Field>
          <div v-if="!readonly" class="flex flex-wrap gap-2">
            <Button
              type="submit"
              variant="primary"
              :loading="save.isPending.value"
              :disabled="!overlay.dirty.value || Boolean(keepError)"
            >
              {{ t("retention.save") }}
            </Button>
            <Button
              :disabled="!overlay.dirty.value || save.isPending.value"
              @click="overlay.reset()"
            >
              {{ t("retention.discard") }}
            </Button>
          </div>
        </form>
      </AsyncState>

      <section
        class="flex flex-col gap-2 rounded-lg bg-surface-sunken p-4 text-sm"
        aria-labelledby="retention-status"
      >
        <h3 id="retention-status" class="font-semibold">{{ t("retention.status.title") }}</h3>
        <AsyncState
          :loading="status.isPending.value"
          :error="status.error.value"
          @retry="status.refetch()"
        >
          <p v-if="!lastRun" class="text-muted">{{ t("retention.status.never") }}</p>
          <div v-else class="flex flex-wrap items-center gap-x-3 gap-y-1">
            <Badge :tone="outcomeTone[lastRun.outcome]">
              {{ t(`retention.status.outcomes.${lastRun.outcome}`) }}
            </Badge>
            <RelativeTime :value="lastRun.attempted_at" />
            <span v-if="lastRun.reason">{{ t(`retention.status.reasons.${lastRun.reason}`) }}</span>
            <span>
              {{
                t("retention.status.removed", {
                  count: lastRun.retired_versions,
                  size: format.bytes(lastRun.logical_bytes),
                })
              }}
            </span>
            <span v-if="lastRun.succeeded_at" class="text-muted">
              {{ t("retention.status.lastSuccess") }}
              <RelativeTime :value="lastRun.succeeded_at" />
            </span>
          </div>
          <p class="text-muted">
            <template v-if="status.data.value?.next_check_at">
              {{ t("retention.status.next") }}
              <RelativeTime :value="status.data.value.next_check_at" />
            </template>
            <template v-else>{{ t("retention.status.notScheduled") }}</template>
          </p>
        </AsyncState>
      </section>

      <section class="flex flex-col gap-3" aria-labelledby="retention-run">
        <div class="flex flex-col gap-1">
          <h3 id="retention-run" class="text-sm font-semibold">{{ t("retention.run.title") }}</h3>
          <p class="text-xs text-muted">{{ t("retention.run.hint") }}</p>
        </div>
        <div class="flex flex-wrap items-center gap-3">
          <Button
            :loading="actions.preview.isPending.value"
            :disabled="
              readonly ||
              overlay.dirty.value ||
              !configuration.data.value ||
              actions.execute.isPending.value
            "
            @click="createPreview"
          >
            {{ t("retention.run.preview") }}
          </Button>
          <span v-if="overlay.dirty.value" class="text-xs text-muted">
            {{ t("retention.run.saveFirst") }}
          </span>
        </div>
        <Alert v-if="notice === 'stale'" tone="warning" :title="t('releases.preview.staleTitle')">
          {{ t("releases.preview.staleDescription") }}
        </Alert>
        <Alert v-if="notice === 'changed'" tone="warning">
          {{ t("retention.run.changed") }}
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
                :disabled="readonly || expired || current.selected_versions === 0"
                @click="execute"
              >
                {{ t("retention.execute") }}
              </Button>
              <Button :disabled="actions.execute.isPending.value" @click="discard(null)">
                {{ t("common.actions.cancel") }}
              </Button>
            </div>
          </template>
        </div>
      </section>
    </div>
  </Card>
</template>
