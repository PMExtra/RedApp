<script setup lang="ts">
import { Check, Monitor, Moon, Sun } from "@lucide/vue";
import { DropdownMenuItemIndicator, DropdownMenuRadioGroup, DropdownMenuRadioItem } from "reka-ui";
import { useI18n } from "vue-i18n";
import { usePreferencesStore, type ThemePreference } from "@/shared/lib/preferences";
import { cn, itemClass } from "./cn";
import DropdownMenu from "./DropdownMenu.vue";
import IconButton from "./IconButton.vue";

const preferences = usePreferencesStore();
const { t } = useI18n();
const options: { value: ThemePreference; icon: typeof Sun }[] = [
  { value: "system", icon: Monitor },
  { value: "light", icon: Sun },
  { value: "dark", icon: Moon },
];

function select(value: unknown) {
  if (value === "system" || value === "light" || value === "dark") preferences.setTheme(value);
}
</script>

<template>
  <DropdownMenu>
    <template #trigger>
      <IconButton :label="t('ui.theme.label')" size="sm" no-tooltip>
        <Moon v-if="preferences.resolvedTheme === 'dark'" aria-hidden="true" />
        <Sun v-else aria-hidden="true" />
      </IconButton>
    </template>
    <DropdownMenuRadioGroup :model-value="preferences.theme" @update:model-value="select">
      <DropdownMenuRadioItem
        v-for="option in options"
        :key="option.value"
        :value="option.value"
        :class="cn(itemClass, 'ps-8')"
      >
        <DropdownMenuItemIndicator class="absolute start-2 inline-flex">
          <Check class="size-4" aria-hidden="true" />
        </DropdownMenuItemIndicator>
        <component :is="option.icon" class="size-4 text-muted" aria-hidden="true" />
        {{ t(`ui.theme.${option.value}`) }}
      </DropdownMenuRadioItem>
    </DropdownMenuRadioGroup>
  </DropdownMenu>
</template>
