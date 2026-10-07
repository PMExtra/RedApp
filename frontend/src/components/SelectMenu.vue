<script setup lang="ts">
import { computed, nextTick, ref, watch } from "vue";
import PopoverPanel from "./PopoverPanel.vue";
import Icon from "./Icon.vue";
import { usePopover } from "../composables/usePopover";
const props = defineProps<{
  modelValue: string;
  options: { value: string; label: string; description?: string; iconText?: string }[];
  label: string;
  disabled?: boolean;
  icon?: boolean;
  topbar?: boolean;
}>();
const emit = defineEmits<{ "update:modelValue": [string] }>();
const {
  id,
  open,
  root,
  trigger,
  close: dismiss,
  show: showPopover,
} = usePopover();
const active = ref(0);
const selected = computed(() =>
  props.options.findIndex((item) => item.value === props.modelValue),
);
const shown = computed(() => props.options[selected.value]?.label || "");
let search = "",
  lastTyped = 0;
function scroll() {
  nextTick(() =>
    document
      .getElementById(`${id}-${active.value}`)
      ?.scrollIntoView?.({ block: "nearest" }),
  );
}
function show() {
  if (props.disabled || !props.options.length) return;
  active.value = Math.max(0, selected.value);
  showPopover();
  scroll();
}
function close(commit = false) {
  if (commit && open.value && props.options[active.value])
    emit("update:modelValue", props.options[active.value]!.value);
  dismiss();
  search = "";
}
function choose(index: number) {
  active.value = index;
  close(true);
  trigger.value?.focus();
}
function key(event: KeyboardEvent) {
  if (props.disabled) return;
  const value = event.key;
  if (value === "Tab") {
    close(true);
    return;
  }
  if (value === "Escape") {
    if (open.value) {
      event.preventDefault();
      event.stopPropagation();
      close();
    }
    return;
  }
  if (
    [
      "Enter",
      " ",
      "ArrowDown",
      "ArrowUp",
      "Home",
      "End",
      "PageDown",
      "PageUp",
    ].includes(value)
  ) {
    event.preventDefault();
    event.stopPropagation();
    const wasOpen = open.value;
    if (!wasOpen) show();
    if (value === "Enter" || value === " ") {
      if (wasOpen) close(true);
      return;
    }
    if (value === "Home") active.value = 0;
    else if (value === "End") active.value = props.options.length - 1;
    else if (wasOpen)
      active.value = Math.max(
        0,
        Math.min(
          props.options.length - 1,
          active.value +
            (["ArrowDown", "PageDown"].includes(value) ? 1 : -1) *
              (value.startsWith("Page") ? 10 : 1),
        ),
      );
    scroll();
    return;
  }
  if (value.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey) {
    event.preventDefault();
    if (!open.value) show();
    const now = Date.now();
    search = now - lastTyped < 700 ? search + value : value;
    lastTyped = now;
    const repeated = [...search].every(
      (c) => c.toLocaleLowerCase() === value.toLocaleLowerCase(),
    );
    const term = (repeated ? value : search).toLocaleLowerCase();
    const start = repeated ? active.value + 1 : active.value;
    for (let n = 0; n < props.options.length; n++) {
      const i = (start + n) % props.options.length;
      if (props.options[i]!.label.toLocaleLowerCase().startsWith(term)) {
        active.value = i;
        scroll();
        break;
      }
    }
  }
}
function blur(event: FocusEvent) {
  if (!root.value?.contains(event.relatedTarget as Node)) close(true);
}
watch(
  () => props.disabled,
  (value) => {
    if (value) close();
  },
);
watch(
  () => props.modelValue,
  () => {
    if (open.value) active.value = Math.max(0, selected.value);
  },
);
watch(
  () => props.options,
  () => {
    if (open.value) {
      active.value = Math.min(active.value, props.options.length - 1);
      if (!props.options.length) close();
    }
  },
);
</script>
<template>
  <div ref="root" class="select-menu" @focusout="blur">
    <button
      ref="trigger"
      type="button"
      class="select-trigger popover-trigger secondary"
      role="combobox"
      aria-haspopup="listbox"
      :aria-label="label"
      :aria-expanded="open"
      :aria-controls="open ? `${id}-list` : undefined"
      :aria-activedescendant="open ? `${id}-${active}` : undefined"
      :disabled="disabled || !options.length"
      @click="open ? close() : show()"
      @keydown="key"
    >
      <Icon v-if="icon" name="globe" :size="16" /><span>{{ shown }}</span
      ><Icon name="chevron" :size="14" />
    </button>
    <PopoverPanel
      as="ul"
      :topbar="topbar"
      v-if="open"
      :id="`${id}-list`"
      class="select-options"
      role="listbox"
      :aria-label="label"
    >
      <li
        v-for="(item, index) in options"
        :key="item.value"
        :id="`${id}-${index}`"
        role="option"
        :data-value="item.value"
        :aria-selected="active === index"
        :class="[
          'popover-option',
          { active: active === index, chosen: item.value === modelValue },
        ]"
        @pointerdown.prevent
        @pointermove="active = index"
        @click="choose(index)"
      >
        <span v-if="item.iconText" class="select-option-text-icon" aria-hidden="true">{{ item.iconText }}</span>
        <span class="select-option-label"
          >{{ item.label
          }}<small v-if="item.description" class="select-description">{{
            item.description
          }}</small></span
        ><span v-if="item.value === modelValue" aria-hidden="true">✓</span>
      </li>
    </PopoverPanel>
  </div>
</template>
