<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import {
  FieldReset,
  isUpstreamRef,
  templateLink,
  OverlayFormActions,
  useAppConfiguration,
  useAppConfigurationPatch,
  useOverlayDraft,
} from "@/features/configuration";
import { useDirtyGuard } from "@/shared/forms";
import { toast } from "@/shared/lib";
import { Alert, AsyncState, Card, RevisionConflictAlert, Switch } from "@/shared/ui";

/**
 * Directory listings (`auto_index`) of a `hosted` or `http-cache` application.
 * Rendered by the application settings page.
 */
const props = defineProps<{ vendor: string; app: string; readOnly?: boolean }>();
const { t } = useI18n();

const configuration = useAppConfiguration(
  () => props.vendor,
  () => props.app,
);
const overlay = useOverlayDraft<{ auto_index: boolean }>(configuration.data, (spec) =>
  spec?.auto_index === undefined ? undefined : { auto_index: spec.auto_index },
);
const { draft } = overlay;
const save = useAppConfigurationPatch(
  () => props.vendor,
  () => props.app,
  configuration.data,
);
const busy = computed(() => save.isPending.value);
useDirtyGuard(overlay.dirty);

function submit() {
  const body = overlay.patch.value;
  if (!body || props.readOnly) return;
  save.mutate(body, {
    onSuccess: () => {
      overlay.reset();
      toast({ tone: "success", title: t("fileIndex.saved") });
    },
  });
}

async function reload() {
  await save.reload();
  overlay.reset();
}
</script>

<template>
  <Card :title="t('fileIndex.title')" :description="t('fileIndex.description')">
    <AsyncState
      :loading="configuration.isPending.value"
      :error="configuration.error.value"
      @retry="configuration.refetch()"
    >
      <form v-if="draft" class="flex flex-col gap-4" novalidate @submit.prevent="submit">
        <Alert
          v-if="
            configuration.data.value?.template_missing &&
            !isUpstreamRef(configuration.data.value?.template_ref)
          "
          tone="warning"
        >
          {{ t("fileIndex.templateMissing") }}
        </Alert>
        <RevisionConflictAlert v-if="save.hasConflict.value" @reload="reload" />
        <fieldset :disabled="readOnly || busy" class="flex items-start gap-3">
          <Switch
            id="file-index-auto-index"
            v-model="draft.auto_index"
            class="mt-0.5"
            aria-describedby="file-index-auto-index-hint"
          />
          <div class="flex min-w-0 flex-1 flex-col gap-1">
            <div class="flex h-6 items-center justify-between gap-2">
              <label for="file-index-auto-index" class="cursor-pointer text-sm font-medium">
                {{ t("fileIndex.label") }}
              </label>
              <FieldReset
                v-bind="templateLink(configuration.data.value, 'auto_index')"
                :origin="configuration.data.value?.fields.auto_index"
                :modified="overlay.modified('auto_index')"
                @reset="overlay.restore('auto_index')"
              />
            </div>
            <p id="file-index-auto-index-hint" class="text-xs text-muted">
              {{ t("fileIndex.hint") }}
            </p>
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
