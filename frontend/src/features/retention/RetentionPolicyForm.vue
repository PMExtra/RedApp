<script setup lang="ts">
import { computed, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  FieldReset,
  OverlayFormActions,
  useAppConfiguration,
  useAppConfigurationPatch,
  useOverlayDraft,
} from "@/features/configuration";
import type { Schema } from "@/shared/api";
import { useDirtyGuard } from "@/shared/forms";
import { confirm, toast } from "@/shared/lib";
import { Alert, AsyncState, Field, NumberInput, RevisionConflictAlert, Switch } from "@/shared/ui";

type Policy = Schema<"RetentionPolicy">;

/** The saved keep-latest policy (part of the application configuration). Emits `update:dirty`. */
const props = defineProps<{ vendor: string; app: string; readOnly?: boolean }>();
const emit = defineEmits<{ "update:dirty": [dirty: boolean] }>();
const { t } = useI18n();

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
watch(overlay.dirty, (dirty) => emit("update:dirty", dirty), { immediate: true });

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
  if (!body || keepError.value || props.readOnly) return;
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
</script>

<template>
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
          class="mt-0.5"
          aria-describedby="retention-enabled-hint"
          :disabled="readOnly || save.isPending.value"
        />
        <div class="flex min-w-0 flex-1 flex-col gap-1">
          <div class="flex h-6 items-center justify-between gap-2">
            <label for="retention-enabled" class="cursor-pointer text-sm font-medium">
              {{ t("retention.enabled") }}
            </label>
            <FieldReset
              :linked="(configuration.data.value?.template_ref ?? null) !== null"
              :origin="configuration.data.value?.fields.retention"
              :modified="overlay.modified('retention')"
              :disabled="readOnly || save.isPending.value"
              @reset="overlay.restore('retention')"
            />
          </div>
          <p id="retention-enabled-hint" class="text-xs text-muted">
            {{ t("retention.enabledHint") }}
          </p>
        </div>
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
          :disabled="readOnly || save.isPending.value"
        />
      </Field>
      <OverlayFormActions
        v-if="!readOnly"
        :dirty="overlay.dirty.value"
        :saving="save.isPending.value"
        :disabled="!overlay.dirty.value || Boolean(keepError)"
        :save-label="t('retention.save')"
        @discard="overlay.reset()"
      />
    </form>
  </AsyncState>
</template>
