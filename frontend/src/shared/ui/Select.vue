<script setup lang="ts">
import { Check, ChevronDown } from "@lucide/vue";
import {
  SelectContent,
  SelectIcon,
  SelectItem,
  SelectItemIndicator,
  SelectItemText,
  SelectPortal,
  SelectRoot,
  SelectTrigger,
  SelectValue,
} from "reka-ui";
import { useI18n } from "vue-i18n";
import { cn, controlClass, itemClass, panelClass } from "./cn";
import type { SelectOption } from "./types";

defineOptions({ inheritAttrs: false });
const model = defineModel<string | undefined>();
defineProps<{
  options: SelectOption[];
  placeholder?: string;
  disabled?: boolean;
  name?: string;
  class?: string;
}>();
const { t } = useI18n();
</script>

<template>
  <SelectRoot v-model="model" :disabled="disabled" :name="name">
    <SelectTrigger
      v-bind="$attrs"
      :class="
        cn(controlClass, 'flex h-9 items-center justify-between gap-2 text-start', $props.class)
      "
    >
      <SelectValue :placeholder="placeholder ?? t('ui.select.placeholder')" class="truncate" />
      <SelectIcon as-child>
        <ChevronDown class="size-4 text-muted" aria-hidden="true" />
      </SelectIcon>
    </SelectTrigger>
    <SelectPortal>
      <!-- No SelectViewport: it injects an inline <style> that the SPA CSP blocks. -->
      <SelectContent
        position="popper"
        :side-offset="4"
        :class="
          cn(
            panelClass,
            'max-h-(--reka-select-content-available-height) w-(--reka-select-trigger-width) overflow-y-auto',
          )
        "
      >
        <SelectItem
          v-for="option in options"
          :key="option.value"
          :value="option.value"
          :disabled="option.disabled"
          :class="cn(itemClass, 'pe-8')"
        >
          <SelectItemText>{{ option.label }}</SelectItemText>
          <SelectItemIndicator class="absolute end-2 inline-flex">
            <Check class="size-4" aria-hidden="true" />
          </SelectItemIndicator>
        </SelectItem>
      </SelectContent>
    </SelectPortal>
  </SelectRoot>
</template>
