<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { z } from "zod";
import {
  FieldReset,
  isEmptyPatch,
  OverlayFormActions,
  useAppConfiguration,
  useAppConfigurationPatch,
  useOverlayForm,
  type AppConfigurationPatch,
} from "@/features/configuration";
import { codePointLength, formError, FormField } from "@/shared/forms";
import { useFormat } from "@/shared/i18n";
import { toast } from "@/shared/lib";
import { AsyncState, Card, RevisionConflictAlert, Textarea } from "@/shared/ui";
import type { App } from "./queries";

/**
 * Per-language usage instructions (Markdown with HTML and scripts, rendered in
 * a sandbox on the public page). Empty stays empty; there is no fallback.
 */
const props = defineProps<{ app: App }>();
const { t } = useI18n();
const format = useFormat();
const MAX = 12000;
const PATHS = ["instructions.en", "instructions.zh-CN"] as const;
const languages = ["en", "zh-CN"] as const;
// Template variables the server replaces when rendering the instructions.
const VARIABLES = {
  baseUrl: "{{base_url}}",
  appPath: "{{app_path}}",
  latest: "{{latest_version}}",
  name: "{{app_name}}",
};

// No control characters except tab, newline and carriage return (spec `Instructions`).
// eslint-disable-next-line no-control-regex -- matching control characters is the point
const CONTROL = /[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/;
const text = z
  .string()
  .refine(
    (value) => codePointLength(value) <= MAX,
    formError("directory.instructions.tooLong", { max: MAX }),
  )
  .refine((value) => !CONTROL.test(value), formError("directory.instructions.control"));
const schema = z.object({ instructions: z.object({ en: text, "zh-CN": text }) });

const configuration = useAppConfiguration(
  () => props.app.vendor_id,
  () => props.app.id,
);
const overlay = useOverlayForm({ configuration: configuration.data, paths: PATHS, schema });
const save = useAppConfigurationPatch(
  () => props.app.vendor_id,
  () => props.app.id,
  configuration.data,
);
const { form } = overlay;
const readOnly = computed(() => props.app.deleted_at !== null);

function length(language: (typeof languages)[number]): number {
  return codePointLength(form.values.instructions[language]);
}

const submit = form.handleSubmit(async (values) => {
  const patch = overlay.patch(values);
  if (isEmptyPatch(patch)) {
    toast({ title: t("configuration.nothingToSave") });
    return;
  }
  try {
    overlay.load(await save.mutateAsync(patch as AppConfigurationPatch));
    toast({ tone: "success", title: t("configuration.saved") });
  } catch {
    // Conflicts keep the draft (alert above); other errors are toasted globally.
  }
});
</script>

<template>
  <Card
    :title="t('directory.instructions.title')"
    :description="t('directory.instructions.description')"
  >
    <AsyncState
      :loading="configuration.isPending.value"
      :error="configuration.error.value"
      @retry="configuration.refetch()"
    >
      <form class="flex flex-col gap-4" novalidate @submit.prevent="submit">
        <RevisionConflictAlert
          v-if="save.hasConflict.value"
          :reloading="configuration.isFetching.value"
          @reload="save.reload()"
        />
        <p class="text-xs text-muted">{{ t("directory.instructions.variables", VARIABLES) }}</p>
        <div v-for="language in languages" :key="language" class="flex items-start gap-2">
          <div class="min-w-0 flex-1">
            <FormField
              v-slot="{ field }"
              :name="`instructions.${language}`"
              :label="
                t('directory.instructions.label', {
                  language: t(`common.languages.${language}`),
                })
              "
              :description="
                t('directory.instructions.count', {
                  count: format.number(length(language)),
                  max: format.number(MAX),
                })
              "
            >
              <Textarea
                v-bind="field"
                :rows="10"
                monospace
                spellcheck="false"
                :lang="language"
                :disabled="readOnly"
              />
            </FormField>
          </div>
          <FieldReset
            v-bind="overlay.resetBinding(`instructions.${language}`)"
            :disabled="readOnly"
            @reset="overlay.reset(`instructions.${language}`)"
          />
        </div>
        <OverlayFormActions
          v-if="!readOnly"
          :dirty="overlay.dirty.value"
          :saving="save.isPending.value"
          @discard="overlay.discard()"
        />
      </form>
    </AsyncState>
  </Card>
</template>
