<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import {
  FieldReset,
  useAppConfiguration,
  useAppConfigurationPatch,
  useOverlayDraft,
} from "@/features/configuration";
import { useDirtyGuard } from "@/shared/forms";
import { useFormat } from "@/shared/i18n";
import { toast } from "@/shared/lib";
import {
  Alert,
  AsyncState,
  Button,
  Card,
  Field,
  NumberInput,
  RevisionConflictAlert,
} from "@/shared/ui";

/**
 * Metadata freshness (`cache_ttl_seconds`) of a release application: how long
 * channel metadata such as "latest" is reused before it is checked again.
 * Rendered by the application settings page.
 */
const props = defineProps<{ vendor: string; app: string; readOnly?: boolean }>();
const { t } = useI18n();
const format = useFormat();
const MIN = 1;
const MAX = 86_400;

const configuration = useAppConfiguration(
  () => props.vendor,
  () => props.app,
);
const overlay = useOverlayDraft<{ cache_ttl_seconds: number | null }>(configuration.data, (spec) =>
  spec?.cache_ttl_seconds === undefined ? undefined : { cache_ttl_seconds: spec.cache_ttl_seconds },
);
const { draft } = overlay;
const save = useAppConfigurationPatch(
  () => props.vendor,
  () => props.app,
  configuration.data,
);
useDirtyGuard(overlay.dirty);

const value = computed({
  get: () => draft.value?.cache_ttl_seconds ?? null,
  set: (next: number | null) => {
    if (draft.value) draft.value = { cache_ttl_seconds: next };
  },
});
const error = computed(() => {
  const seconds = value.value;
  if (seconds === null) return t("cachePolicy.ttl.required");
  return Number.isInteger(seconds) && seconds >= MIN && seconds <= MAX
    ? undefined
    : t("cachePolicy.ttl.range", { min: MIN, max: MAX });
});
const templateValue = computed(() => {
  const seconds = overlay.template.value?.cache_ttl_seconds;
  return seconds === undefined || seconds === null ? undefined : format.duration(seconds);
});

function submit() {
  const body = overlay.patch.value;
  if (!body || error.value || props.readOnly) return;
  save.mutate(body, {
    onSuccess: () => {
      overlay.reset();
      toast({ tone: "success", title: t("cachePolicy.ttl.saved") });
    },
  });
}

async function reload() {
  await save.reload();
  overlay.reset();
}
</script>

<template>
  <Card :title="t('cachePolicy.ttl.title')" :description="t('cachePolicy.ttl.description')">
    <AsyncState
      :loading="configuration.isPending.value"
      :error="configuration.error.value"
      @retry="configuration.refetch()"
    >
      <form v-if="draft" class="flex flex-col gap-4" novalidate @submit.prevent="submit">
        <Alert v-if="configuration.data.value?.template_missing" tone="warning">
          {{ t("cachePolicy.templateMissing") }}
        </Alert>
        <RevisionConflictAlert v-if="save.hasConflict.value" @reload="reload" />
        <div class="flex items-start gap-2">
          <Field
            v-slot="{ control }"
            class="w-64"
            :label="t('cachePolicy.ttl.label')"
            :description="t('cachePolicy.ttl.hint')"
            :error="overlay.dirty.value ? error : undefined"
            required
          >
            <NumberInput
              v-bind="control"
              v-model="value"
              :min="MIN"
              :max="MAX"
              :step="1"
              :unit="t('cachePolicy.ttl.unit')"
              :disabled="readOnly || save.isPending.value"
            />
          </Field>
          <FieldReset
            class="mt-7"
            :linked="(configuration.data.value?.template_ref ?? null) !== null"
            :origin="configuration.data.value?.fields.cache_ttl_seconds"
            :modified="overlay.modified('cache_ttl_seconds')"
            :template-value="templateValue"
            :disabled="readOnly || save.isPending.value"
            @reset="overlay.restore('cache_ttl_seconds')"
          />
        </div>
        <div v-if="!readOnly" class="flex flex-wrap gap-2">
          <Button
            type="submit"
            variant="primary"
            :loading="save.isPending.value"
            :disabled="!overlay.dirty.value || Boolean(error)"
          >
            {{ t("common.actions.save") }}
          </Button>
          <Button :disabled="!overlay.dirty.value || save.isPending.value" @click="overlay.reset()">
            {{ t("cachePolicy.discard") }}
          </Button>
        </div>
      </form>
    </AsyncState>
  </Card>
</template>
