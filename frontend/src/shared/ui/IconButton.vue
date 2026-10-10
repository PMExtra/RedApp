<script setup lang="ts">
import Button from "./Button.vue";
import type { ButtonSize, ButtonVariant } from "./types";
import Tooltip from "./Tooltip.vue";

// Listeners and attributes go to the button itself: with the tooltip, the
// root is Reka's renderless TooltipRoot, which drops fallthrough attributes.
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
    icon
    :variant="variant"
    :size="size"
    :disabled="disabled"
    :loading="loading"
    :type="type"
    :aria-label="label"
    v-bind="$attrs"
  >
    <slot v-if="!loading" />
  </Button>
  <Tooltip v-else :content="label">
    <Button
      icon
      :variant="variant"
      :size="size"
      :disabled="disabled"
      :loading="loading"
      :type="type"
      :aria-label="label"
      v-bind="$attrs"
    >
      <slot v-if="!loading" />
    </Button>
  </Tooltip>
</template>
