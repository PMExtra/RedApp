<script setup lang="ts">
import Button from "./Button.vue";
import type { ButtonSize, ButtonVariant } from "./types";
import Tooltip from "./Tooltip.vue";

// Attributes and listeners (`@click`, `class`) go to the button: with a
// tooltip the root is a renderless Reka component that would drop them.
defineOptions({ inheritAttrs: false });
withDefaults(
  defineProps<{
    /** Accessible name; also shown as a tooltip. */
    label: string;
    variant?: ButtonVariant;
    size?: ButtonSize;
    disabled?: boolean;
    loading?: boolean;
    type?: "button" | "submit" | "reset";
    /** Hide the tooltip (for example inside a menu trigger that has its own). */
    noTooltip?: boolean;
  }>(),
  { variant: "ghost", size: "md", type: "button" },
);
</script>

<template>
  <Button
    v-if="noTooltip"
    v-bind="$attrs"
    icon
    :variant="variant"
    :size="size"
    :disabled="disabled"
    :loading="loading"
    :type="type"
    :aria-label="label"
  >
    <slot v-if="!loading" />
  </Button>
  <Tooltip v-else :content="label">
    <Button
      v-bind="$attrs"
      icon
      :variant="variant"
      :size="size"
      :disabled="disabled"
      :loading="loading"
      :type="type"
      :aria-label="label"
    >
      <slot v-if="!loading" />
    </Button>
  </Tooltip>
</template>
