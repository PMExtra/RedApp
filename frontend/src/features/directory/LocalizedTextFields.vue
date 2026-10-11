<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { FieldReset } from "@/features/configuration";
import { FormField } from "@/shared/forms";
import { Input, Textarea } from "@/shared/ui";
import type { ResetBinding } from "./types";

/**
 * Bilingual name and description (`name.en`, `name.zh-CN`, `description.*`)
 * of a vee-validate form, grouped by language.
 */
defineProps<{
  disabled?: boolean;
  resetBinding?: (path: string) => ResetBinding;
}>();
const emit = defineEmits<{ reset: [path: string] }>();
const { t } = useI18n();
const languages = ["en", "zh-CN"] as const;
</script>

<template>
  <div class="grid gap-4 md:grid-cols-2">
    <fieldset
      v-for="language in languages"
      :key="language"
      class="flex flex-col gap-3 rounded-lg border border-border p-4"
      :disabled="disabled"
      :lang="language"
    >
      <legend class="px-1 text-sm font-medium">{{ t(`common.languages.${language}`) }}</legend>
      <FormField
        :name="`name.${language}`"
        :label="t('directory.fields.nameIn', { language: t(`common.languages.${language}`) })"
        required
      >
        <template v-if="resetBinding" #label-end>
          <FieldReset
            v-bind="resetBinding(`name.${language}`)"
            :disabled="disabled"
            @reset="emit('reset', `name.${language}`)"
          />
        </template>
        <template #default="{ field }">
          <Input v-bind="field" autocomplete="off" :disabled="disabled" />
        </template>
      </FormField>
      <FormField
        :name="`description.${language}`"
        :label="
          t('directory.fields.descriptionIn', { language: t(`common.languages.${language}`) })
        "
      >
        <template v-if="resetBinding" #label-end>
          <FieldReset
            v-bind="resetBinding(`description.${language}`)"
            :disabled="disabled"
            @reset="emit('reset', `description.${language}`)"
          />
        </template>
        <template #default="{ field }">
          <Textarea v-bind="field" :rows="3" :disabled="disabled" />
        </template>
      </FormField>
    </fieldset>
  </div>
</template>
