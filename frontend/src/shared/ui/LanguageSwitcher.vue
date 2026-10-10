<script setup lang="ts">
import { Check, Languages } from "@lucide/vue";
import { DropdownMenuRadioGroup, DropdownMenuRadioItem, DropdownMenuItemIndicator } from "reka-ui";
import { useI18n } from "vue-i18n";
import { SUPPORTED_LOCALES, isLocale } from "@/shared/i18n";
import { usePreferencesStore } from "@/shared/lib/preferences";
import { itemClass, cn } from "./cn";
import Button from "./Button.vue";
import DropdownMenu from "./DropdownMenu.vue";

const preferences = usePreferencesStore();
const { t } = useI18n();

function select(value: unknown) {
  if (isLocale(value)) preferences.setLocale(value);
}
</script>

<template>
  <DropdownMenu>
    <template #trigger>
      <Button variant="ghost" size="sm" :aria-label="t('ui.language.label')">
        <Languages aria-hidden="true" />
        <span aria-hidden="true">{{ preferences.locale === "zh-CN" ? "中" : "EN" }}</span>
      </Button>
    </template>
    <DropdownMenuRadioGroup :model-value="preferences.locale" @update:model-value="select">
      <DropdownMenuRadioItem
        v-for="locale in SUPPORTED_LOCALES"
        :key="locale"
        :value="locale"
        :lang="locale"
        :class="cn(itemClass, 'ps-8')"
      >
        <DropdownMenuItemIndicator class="absolute start-2 inline-flex">
          <Check class="size-4" aria-hidden="true" />
        </DropdownMenuItemIndicator>
        {{ t(`common.languages.${locale}`) }}
      </DropdownMenuRadioItem>
    </DropdownMenuRadioGroup>
  </DropdownMenu>
</template>
