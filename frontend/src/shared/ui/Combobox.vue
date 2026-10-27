<script setup lang="ts" generic="T extends ComboboxOption">
import { computed, nextTick, ref, watch } from "vue";
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
  /** Clear the selection and the typed text after choosing (pickers that add to a list). */
  clearOnSelect?: boolean;
  /**
   * Search boxes: Reka's automatic first-item highlight does not count, so
   * Enter submits the typed text until the arrow keys move through the list.
   */
  manualHighlight?: boolean;
  /** Show the suggestions on focus or click, before anything is typed. */
  openOnFocus?: boolean;
  class?: string;
}>();
const emit = defineEmits<{
  select: [option: T];
  /** Enter pressed while the list is closed or nothing is highlighted. */
  submit: [search: string];
}>();
defineSlots<{
  option?: (props: { option: T }) => unknown;
  /** Decorative icon inside the input's start edge. */
  leading?: () => unknown;
}>();

const { t } = useI18n();
const open = ref(false);
// Reka keeps reporting the last highlighted value after the options change
// (for example when the list empties), so only a listed option counts.
const highlighted = ref<string | undefined>();
const hasHighlight = computed(
  () =>
    highlighted.value !== undefined &&
    props.options.some((option) => option.value === highlighted.value && !option.disabled),
);

// Set by the arrow keys; typing or closing hands Enter back to the text.
const navigated = ref(false);
watch([search, open], () => {
  navigated.value = false;
});
const activeHighlight = computed(
  () => hasHighlight.value && (!props.manualHighlight || navigated.value),
);

function onArrow(event: KeyboardEvent) {
  if (!props.manualHighlight || navigated.value || !open.value || event.isComposing) return;
  // The first arrow starts at an end of the list instead of moving past the
  // hidden automatic highlight (Reka handles Home/End on the input).
  event.preventDefault();
  event.stopImmediatePropagation();
  navigated.value = true;
  const key = event.key === "ArrowUp" ? "End" : "Home";
  event.currentTarget?.dispatchEvent(new KeyboardEvent("keydown", { key, bubbles: true }));
}

function onSelect(value: unknown) {
  const option = props.options.find((item) => item.value === value);
  if (!option) return;
  emit("select", option);
  // Cleared on the next tick: Reka writes the chosen value into the input
  // after the selection, and a same-tick reset never reaches its state.
  if (props.clearOnSelect) {
    void nextTick(() => {
      model.value = undefined;
      search.value = "";
    });
  }
}

function onEnter(event: KeyboardEvent) {
  // Enter that confirms an IME composition must not submit.
  if (event.isComposing) return;
  if (open.value && activeHighlight.value) return; // Reka chooses the highlighted option.
  if (props.manualHighlight) {
    // Keep Reka from choosing an automatic highlight the user never moved to.
    if (open.value && hasHighlight.value) event.stopImmediatePropagation();
    open.value = false;
  }
  emit("submit", search.value);
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
    :open-on-focus="openOnFocus"
    :open-on-click="openOnFocus"
    @update:model-value="onSelect"
    @highlight="highlighted = $event?.value as string | undefined"
  >
    <ComboboxAnchor :class="cn('relative flex items-center', $props.class)">
      <ComboboxInput
        v-bind="$attrs"
        v-model="search"
        :placeholder="placeholder"
        :class="cn(controlClass, 'h-9 pe-8', $slots.leading && 'ps-8')"
        @keydown.enter.capture="onEnter"
        @keydown.up.capture="onArrow"
        @keydown.down.capture="onArrow"
      />
      <span
        v-if="$slots.leading"
        class="pointer-events-none absolute start-2.5 flex text-subtle [&>svg]:size-4"
        aria-hidden="true"
      >
        <slot name="leading" />
      </span>
      <Spinner v-if="loading" size="sm" class="pointer-events-none absolute end-2 text-muted" />
    </ComboboxAnchor>
    <ComboboxPortal>
      <!-- No ComboboxViewport: it injects an inline <style> that the SPA CSP blocks. -->
      <ComboboxContent
        position="popper"
        :side-offset="4"
        :collision-padding="8"
        :class="
          cn(
            panelClass,
            'max-h-[min(20rem,var(--reka-combobox-content-available-height))] w-(--reka-combobox-trigger-width) max-w-[calc(100vw-1rem)] overflow-y-auto',
            // Hide the automatic highlight; hovering still shows.
            manualHighlight &&
              !navigated &&
              '[&_[data-highlighted]:not(:hover)]:bg-transparent [&_[role=option]:hover]:bg-surface-hover',
          )
        "
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
