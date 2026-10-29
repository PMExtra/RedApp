<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import {
  FieldReset,
  OverlayFormActions,
  templateLink,
  useAppConfiguration,
  useAppConfigurationPatch,
  useOverlayDraft,
} from "@/features/configuration";
import { useDirtyGuard } from "@/shared/forms";
import { toast } from "@/shared/lib";
import {
  AsyncState,
  Card,
  Checkbox,
  Field,
  RevisionConflictAlert,
  Select,
  type SelectOption,
} from "@/shared/ui";
import { useReleaseArtifacts } from "./queries";

/**
 * Download choices of a JSON release application: the package recommended
 * when the visitor's environment does not pick one, and the packages the
 * public page offers. Packages come from the current releases; keys of
 * packages no longer released stay listed until they are cleared.
 */
const props = defineProps<{ vendor: string; app: string; readOnly?: boolean }>();
const { t } = useI18n();
// Artifact keys never start with "-", so it can stand for "no default".
const NONE = "-";

const configuration = useAppConfiguration(
  () => props.vendor,
  () => props.app,
);
const artifacts = useReleaseArtifacts(
  () => props.vendor,
  () => props.app,
);
const overlay = useOverlayDraft<{ default_artifact: string; hidden_artifacts: string[] }>(
  configuration.data,
  (spec) =>
    spec?.default_artifact === undefined || spec.hidden_artifacts === undefined
      ? undefined
      : { default_artifact: spec.default_artifact, hidden_artifacts: spec.hidden_artifacts },
);
const { draft } = overlay;
const save = useAppConfigurationPatch(
  () => props.vendor,
  () => props.app,
  configuration.data,
);
const busy = computed(() => save.isPending.value);
useDirtyGuard(overlay.dirty);

const known = computed(() => artifacts.data.value?.items ?? []);
const label = (a: (typeof known.value)[number]) =>
  [t(`releaseDownloads.os.${a.os}`), a.arch, a.format, a.scope, a.variant]
    .filter(Boolean)
    .join(" · ");
/** Configured keys of packages the current releases do not have. */
const unknown = computed(() => {
  const keys = new Set(known.value.map((a) => a.key));
  const configured = [
    draft.value?.default_artifact ?? "",
    ...(draft.value?.hidden_artifacts ?? []),
  ];
  return [...new Set(configured.filter((key) => key && !keys.has(key)))];
});
const defaultOptions = computed<SelectOption[]>(() => [
  { value: NONE, label: t("downloadChoices.noDefault") },
  ...known.value.map((a) => ({ value: a.key, label: `${label(a)} (${a.key})` })),
  ...unknown.value.map((key) => ({ value: key, label: t("downloadChoices.missing", { key }) })),
]);
const defaultValue = computed({
  get: () => draft.value?.default_artifact || NONE,
  set: (value: string | undefined) => {
    if (draft.value) draft.value.default_artifact = !value || value === NONE ? "" : value;
  },
});
function offered(key: string): boolean {
  return !draft.value?.hidden_artifacts.includes(key);
}
function setOffered(key: string, value: boolean | "indeterminate") {
  if (!draft.value) return;
  const hidden = draft.value.hidden_artifacts.filter((k) => k !== key);
  draft.value.hidden_artifacts = value === true ? hidden : [...hidden, key];
}

function submit() {
  const body = overlay.patch.value;
  if (!body || props.readOnly) return;
  save.mutate(body, {
    onSuccess: () => {
      overlay.reset();
      toast({ tone: "success", title: t("downloadChoices.saved") });
    },
  });
}

async function reload() {
  await save.reload();
  overlay.reset();
}
</script>

<template>
  <Card :title="t('downloadChoices.title')" :description="t('downloadChoices.description')">
    <AsyncState
      :loading="configuration.isPending.value"
      :error="configuration.error.value"
      @retry="configuration.refetch()"
    >
      <form v-if="draft" class="flex flex-col gap-4" novalidate @submit.prevent="submit">
        <RevisionConflictAlert v-if="save.hasConflict.value" @reload="reload" />
        <fieldset :disabled="readOnly || busy" class="flex flex-col gap-4">
          <Field
            :label="t('downloadChoices.default')"
            :description="t('downloadChoices.defaultHint')"
          >
            <template #default="{ control }">
              <div class="flex items-center gap-2">
                <Select
                  v-bind="control"
                  v-model="defaultValue"
                  :options="defaultOptions"
                  class="max-w-md flex-1"
                />
                <FieldReset
                  v-bind="templateLink(configuration.data.value, 'default_artifact')"
                  :origin="configuration.data.value?.fields.default_artifact"
                  :modified="overlay.modified('default_artifact')"
                  @reset="overlay.restore('default_artifact')"
                />
              </div>
            </template>
          </Field>
          <div class="flex flex-col gap-2">
            <div class="flex items-center justify-between gap-2">
              <span class="text-sm font-medium">{{ t("downloadChoices.offered") }}</span>
              <FieldReset
                v-bind="templateLink(configuration.data.value, 'hidden_artifacts')"
                :origin="configuration.data.value?.fields.hidden_artifacts"
                :modified="overlay.modified('hidden_artifacts')"
                @reset="overlay.restore('hidden_artifacts')"
              />
            </div>
            <p class="text-xs text-muted">{{ t("downloadChoices.offeredHint") }}</p>
            <AsyncState
              :loading="artifacts.isPending.value"
              :error="artifacts.error.value"
              @retry="artifacts.refetch()"
            >
              <div class="flex flex-col gap-2">
                <p v-if="known.length === 0" class="text-sm text-muted">
                  {{ t("downloadChoices.none") }}
                </p>
                <Checkbox
                  v-for="artifact in known"
                  :key="artifact.key"
                  :model-value="offered(artifact.key)"
                  :label="`${label(artifact)} (${artifact.key})`"
                  @update:model-value="setOffered(artifact.key, $event)"
                />
                <Checkbox
                  v-for="key in unknown.filter((k) => !offered(k))"
                  :key="key"
                  :model-value="false"
                  :label="t('downloadChoices.missing', { key })"
                  @update:model-value="setOffered(key, $event)"
                />
              </div>
            </AsyncState>
          </div>
        </fieldset>
        <OverlayFormActions
          v-if="!readOnly"
          :dirty="overlay.dirty.value"
          :saving="busy"
          :disabled="!overlay.dirty.value"
          @discard="overlay.reset()"
        />
      </form>
    </AsyncState>
  </Card>
</template>
