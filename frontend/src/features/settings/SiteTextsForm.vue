<script setup lang="ts">
import { watch } from "vue";
import { useForm } from "vee-validate";
import { useI18n } from "vue-i18n";
import { z } from "zod";
import { FormField, formError, useDirtyGuard, zodSchema } from "@/shared/forms";
import { toast } from "@/shared/lib";
import { AsyncState, Button, Card, Input, RevisionConflictAlert, Textarea } from "@/shared/ui";
import { useSaveSiteSettings, useSiteSettings, type SiteSettings } from "./queries";

const LANGUAGES = ["en", "zh-CN"] as const;
const LIMITS = { title: 80, subtitle: 160, disclaimer: 500 } as const;
// The server allows newline and tab, no other control characters.
// eslint-disable-next-line no-control-regex
const CONTROL = /[\u0000-\u0008\u000B-\u001F\u007F]/;

function text(max: number, required: boolean) {
  let schema = z
    .string()
    .trim()
    .max(max, formError("ui.form.tooLong", { max }))
    .refine((value) => !CONTROL.test(value), formError("settings.site.controlCharacters"));
  if (required) schema = schema.min(1, formError("settings.site.titleRequired"));
  return schema;
}
function localized(max: number, required = false) {
  return z.object({ en: text(max, required), "zh-CN": text(max, required) });
}
const schema = z.object({
  title: localized(LIMITS.title, true),
  subtitle: localized(LIMITS.subtitle),
  disclaimer: localized(LIMITS.disclaimer),
});

const { t } = useI18n();
const settings = useSiteSettings();
const save = useSaveSiteSettings(settings.data);

function draftOf(state: SiteSettings): SiteSettings {
  return {
    title: { ...state.title },
    subtitle: { ...state.subtitle },
    disclaimer: { ...state.disclaimer },
  };
}

const { handleSubmit, resetForm, meta } = useForm({ validationSchema: zodSchema(schema) });
useDirtyGuard(() => meta.value.dirty);

// Adopt the server state unless the user has unsaved edits.
watch(
  () => settings.data.value,
  (state) => {
    if (state && !meta.value.dirty) resetForm({ values: draftOf(state) });
  },
  { immediate: true },
);

const submit = handleSubmit((values) => {
  save.mutate(values, {
    onSuccess: (state) => {
      resetForm({ values: draftOf(state) });
      toast({ tone: "success", title: t("settings.site.saved") });
    },
  });
});

async function reload() {
  await save.reload();
  if (settings.data.value) resetForm({ values: draftOf(settings.data.value) });
}

function discard() {
  if (settings.data.value) resetForm({ values: draftOf(settings.data.value) });
}
</script>

<template>
  <Card :title="t('settings.site.title')" :description="t('settings.site.description')">
    <AsyncState
      :loading="settings.isPending.value"
      :error="settings.data.value ? undefined : settings.error.value"
      @retry="settings.refetch()"
    >
      <form class="flex flex-col gap-5" novalidate @submit.prevent="submit">
        <RevisionConflictAlert v-if="save.hasConflict.value" @reload="reload" />
        <div class="grid gap-6 md:grid-cols-2">
          <fieldset
            v-for="language in LANGUAGES"
            :key="language"
            class="flex flex-col gap-4"
            :disabled="save.isPending.value"
          >
            <legend class="mb-2 text-sm font-semibold">
              {{ t(`common.languages.${language}`) }}
            </legend>
            <FormField
              v-slot="{ field }"
              :name="`title.${language}`"
              :label="t('settings.site.fields.title')"
              required
            >
              <Input v-bind="field" :lang="language" :maxlength="LIMITS.title" />
            </FormField>
            <FormField
              v-slot="{ field }"
              :name="`subtitle.${language}`"
              :label="t('settings.site.fields.subtitle')"
            >
              <Input v-bind="field" :lang="language" :maxlength="LIMITS.subtitle" />
            </FormField>
            <FormField
              v-slot="{ field }"
              :name="`disclaimer.${language}`"
              :label="t('settings.site.fields.disclaimer')"
              :description="t('settings.site.fields.disclaimerHint', { max: LIMITS.disclaimer })"
            >
              <Textarea v-bind="field" :lang="language" :rows="4" :maxlength="LIMITS.disclaimer" />
            </FormField>
          </fieldset>
        </div>
        <div class="flex flex-wrap justify-end gap-2">
          <Button v-if="meta.dirty" :disabled="save.isPending.value" @click="discard">
            {{ t("settings.discard") }}
          </Button>
          <Button
            type="submit"
            variant="primary"
            :loading="save.isPending.value"
            :disabled="!meta.dirty"
          >
            {{ t("settings.site.save") }}
          </Button>
        </div>
      </form>
    </AsyncState>
  </Card>
</template>
