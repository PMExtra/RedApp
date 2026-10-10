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
      <div class="flex items-start gap-2">
        <div class="min-w-0 flex-1">
          <FormField
            v-slot="{ field }"
            :name="`name.${language}`"
            :label="t('directory.fields.nameIn', { language: t(`common.languages.${language}`) })"
            required
          >
            <Input v-bind="field" autocomplete="off" :disabled="disabled" />
          </FormField>
        </div>
        <FieldReset
          v-if="resetBinding"
          v-bind="resetBinding(`name.${language}`)"
          :disabled="disabled"
          @reset="emit('reset', `name.${language}`)"
        />
      </div>
      <div class="flex items-start gap-2">
        <div class="min-w-0 flex-1">
          <FormField
            v-slot="{ field }"
            :name="`description.${language}`"
            :label="
              t('directory.fields.descriptionIn', { language: t(`common.languages.${language}`) })
            "
          >
            <Textarea v-bind="field" :rows="3" :disabled="disabled" />
          </FormField>
        </div>
        <FieldReset
          v-if="resetBinding"
          v-bind="resetBinding(`description.${language}`)"
          :disabled="disabled"
          @reset="emit('reset', `description.${language}`)"
        />
      </div>
    </fieldset>
  </div>
</template>
