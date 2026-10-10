<script setup lang="ts" generic="T extends ComboboxOption">
import { ref } from "vue";
import {
  ComboboxAnchor,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxPortal,
  ComboboxRoot,
} from "reka-ui";
import { useI18n } from "vue-i18n";
import { cn, controlClass, itemClass, panelClass } from "./cn";
import Spinner from "./Spinner.vue";
import type { ComboboxOption } from "./types";

defineOptions({ inheritAttrs: false });
/** Selected option value. */
const model = defineModel<string | undefined>();
/** Text typed by the user; fetch suggestions for it (debounce in the caller). */
const search = defineModel<string>("search", { default: "" });
const props = defineProps<{
  /** Suggestions for the current search; filtering happens on the server. */
  options: T[];
  loading?: boolean;
  placeholder?: string;
  disabled?: boolean;
  /** Keep the typed text after choosing an option (search boxes). */
  keepSearch?: boolean;
  class?: string;
}>();
const emit = defineEmits<{
  select: [option: T];
  /** Enter pressed while the list is closed or nothing is highlighted. */
  submit: [search: string];
}>();
defineSlots<{ option?: (props: { option: T }) => unknown }>();

const { t } = useI18n();
const open = ref(false);
const highlighted = ref<string | undefined>();

function onSelect(value: unknown) {
  const option = props.options.find((item) => item.value === value);
  if (option) emit("select", option);
}

function onEnter(event: KeyboardEvent) {
  // Enter that confirms an IME composition must not submit.
  if (event.isComposing) return;
  if (!open.value || highlighted.value === undefined) emit("submit", search.value);
}
</script>

<template>
  <ComboboxRoot
    v-model="model"
    v-model:open="open"
    ignore-filter
    :reset-search-term-on-blur="false"
    :reset-search-term-on-select="!keepSearch"
    :disabled="disabled"
    @update:model-value="onSelect"
    @highlight="highlighted = $event?.value as string | undefined"
  >
    <ComboboxAnchor :class="cn('relative flex items-center', $props.class)">
      <ComboboxInput
        v-bind="$attrs"
        v-model="search"
        :placeholder="placeholder"
        :class="cn(controlClass, 'h-9 pe-8')"
        @keydown.enter.capture="onEnter"
      />
      <Spinner v-if="loading" size="sm" class="pointer-events-none absolute end-2 text-muted" />
    </ComboboxAnchor>
    <ComboboxPortal>
      <!-- No ComboboxViewport: it injects an inline <style> that the SPA CSP blocks. -->
      <ComboboxContent
        position="popper"
        :side-offset="4"
        :class="cn(panelClass, 'max-h-80 w-(--reka-combobox-trigger-width) overflow-y-auto')"
      >
        <ComboboxEmpty class="px-2 py-1.5 text-muted">
          {{ loading ? t("ui.combobox.loading") : t("ui.combobox.noResults") }}
        </ComboboxEmpty>
        <ComboboxItem
          v-for="option in options"
          :key="option.value"
          :value="option.value"
          :text-value="option.label"
          :disabled="option.disabled"
          :class="itemClass"
        >
          <slot name="option" :option="option">{{ option.label }}</slot>
        </ComboboxItem>
      </ComboboxContent>
    </ComboboxPortal>
  </ComboboxRoot>
</template>
