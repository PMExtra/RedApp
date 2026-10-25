<script setup lang="ts">
import { Trash2 } from "@lucide/vue";
import { computed } from "vue";
import { useFieldArray, useForm } from "vee-validate";
import { useI18n } from "vue-i18n";
import { z } from "zod";
import { FormField, formError, zodSchema } from "@/shared/forms";
import { toast } from "@/shared/lib";
import {
  AsyncState,
  Button,
  Card,
  Checkbox,
  Input,
  IconButton,
  RevisionConflictAlert,
  SortableList,
  Textarea,
} from "@/shared/ui";
import { useSaveSiteSettings, useSiteSettings, type SiteSettings } from "./queries";
import { validNavigationURL } from "./validation";
import { useServerDraft } from "./serverDraft";

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
  navigation: z
    .array(
      z.object({
        localized_title: z
          .object({
            en: text(80, false).refine(
              (value) => !/[\r\n\t]/.test(value),
              formError("settings.site.controlCharacters"),
            ),
            "zh-CN": text(80, false).refine(
              (value) => !/[\r\n\t]/.test(value),
              formError("settings.site.controlCharacters"),
            ),
          })
          .refine((value) => Boolean(value.en || value["zh-CN"]), {
            message: formError("settings.site.titleRequired"),
            path: ["zh-CN"],
          }),
        url: z.string().refine(validNavigationURL, formError("settings.navigation.invalidURL")),
        new_tab: z.boolean(),
      }),
    )
    .max(20)
    .default([]),
  title: localized(LIMITS.title, true),
  subtitle: localized(LIMITS.subtitle),
  disclaimer: localized(LIMITS.disclaimer),
});

const { t } = useI18n();
const settings = useSiteSettings();
const save = useSaveSiteSettings(settings.data);

function draftOf(state: SiteSettings) {
  return {
    navigation: (state.navigation ?? []).map((item) => ({
      ...item,
      localized_title:
        item.localized_title && (item.localized_title.en || item.localized_title["zh-CN"])
          ? { ...item.localized_title }
          : { en: item.title, "zh-CN": item.title },
    })),
    title: { ...state.title },
    subtitle: { ...state.subtitle },
    disclaimer: { ...state.disclaimer },
  };
}

const { handleSubmit, resetForm, meta } = useForm({ validationSchema: zodSchema(schema) });
const entries = useFieldArray<z.infer<typeof schema>["navigation"][number]>("navigation");
const fields = entries.fields;
const navigation = computed({
  get: () => [...fields.value],
  set: (ordered) => {
    ordered.forEach((item, index) => {
      const from = fields.value.findIndex((field) => field.key === item.key);
      if (from !== index) entries.move(from, index);
    });
  },
});
const baseline = useServerDraft({
  state: () => settings.data.value,
  dirty: () => meta.value.dirty,
  adopt: (state) => {
    resetForm({ values: draftOf(state) });
  },
});

const submit = handleSubmit((values) => {
  save.mutate(
    {
      ...values,
      navigation: values.navigation.map((item) => ({
        ...item,
        title: item.localized_title["zh-CN"] || item.localized_title.en,
      })),
    },
    {
      onSuccess: (state) => {
        baseline.reset(state);
        toast({ tone: "success", title: t("settings.site.saved") });
      },
    },
  );
});

async function reload() {
  await save.reload();
  baseline.reset();
}

function discard() {
  baseline.reset();
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
        <fieldset class="flex min-w-0 flex-col gap-4" :disabled="save.isPending.value">
          <legend class="mb-2 text-sm font-semibold">{{ t("settings.navigation.title") }}</legend>
          <p class="text-sm text-muted">{{ t("settings.navigation.description") }}</p>
          <SortableList
            v-model="navigation"
            :item-key="(item) => String(item.key)"
            :item-label="
              (item) =>
                item.value.localized_title?.['zh-CN'] ||
                item.value.localized_title?.en ||
                t('settings.navigation.untitled')
            "
            :disabled="save.isPending.value"
          >
            <template #item="{ index }">
              <div
                class="grid min-w-0 grid-cols-1 items-start gap-3 sm:grid-cols-2 xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,2fr)_auto_auto]"
              >
                <FormField
                  v-for="language in ['zh-CN', 'en'] as const"
                  :key="language"
                  v-slot="{ field }"
                  :name="`navigation[${index}].localized_title.${language}`"
                  :label="t(`settings.navigation.labels.${language}`)"
                >
                  <Input v-bind="field" :lang="language" :maxlength="80" />
                </FormField>
                <FormField
                  v-slot="{ field }"
                  :name="`navigation[${index}].url`"
                  :label="t('settings.navigation.url')"
                  required
                >
                  <Input v-bind="field" :maxlength="2048" />
                </FormField>
                <FormField
                  v-slot="{ field }"
                  :name="`navigation[${index}].new_tab`"
                  :label="t('settings.navigation.newTab')"
                >
                  <div class="flex h-9 items-center">
                    <Checkbox
                      :id="field.id"
                      :model-value="field.modelValue"
                      @update:model-value="field['onUpdate:modelValue']"
                    />
                  </div>
                </FormField>
                <IconButton
                  class="self-end"
                  :label="t('settings.navigation.remove')"
                  :disabled="save.isPending.value"
                  @click="entries.remove(index)"
                >
                  <Trash2 aria-hidden="true" />
                </IconButton>
              </div>
            </template>
          </SortableList>
          <Button
            :disabled="navigation.length >= 20 || save.isPending.value"
            @click="
              entries.push({ localized_title: { en: '', 'zh-CN': '' }, url: '', new_tab: false })
            "
            >{{ t("settings.navigation.add") }}</Button
          >
        </fieldset>
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
