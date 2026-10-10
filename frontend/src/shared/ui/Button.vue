<script setup lang="ts">
import { computed } from "vue";
import { Primitive } from "reka-ui";
import { cn } from "./cn";
import Spinner from "./Spinner.vue";
import type { ButtonSize, ButtonVariant } from "./types";

const props = withDefaults(
  defineProps<{
    variant?: ButtonVariant;
    size?: ButtonSize;
    /** Shows a spinner and blocks clicks; keeps the label for screen readers. */
    loading?: boolean;
    disabled?: boolean;
    type?: "button" | "submit" | "reset";
    /** Render the single child (for example a RouterLink) with button styling. */
    asChild?: boolean;
    /** Square button for a single icon (prefer IconButton, which adds a label). */
    icon?: boolean;
  }>(),
  { variant: "secondary", size: "md", type: "button" },
);

const variants: Record<ButtonVariant, string> = {
  primary: "bg-primary text-primary-fg hover:bg-primary-hover",
  secondary: "border border-border-strong bg-surface text-fg shadow-sm hover:bg-surface-hover",
  ghost: "text-fg hover:bg-surface-hover",
  danger: "bg-danger text-danger-fg hover:bg-danger-hover",
};

const classes = computed(() =>
  cn(
    "inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-md font-medium",
    "transition-colors focus-ring disabled:cursor-not-allowed disabled:opacity-50",
    "aria-disabled:cursor-not-allowed aria-disabled:opacity-50 [&_svg]:size-4 [&_svg]:shrink-0",
    variants[props.variant],
    props.icon
      ? props.size === "sm"
        ? "size-8"
        : "size-9"
      : props.size === "sm"
        ? "h-8 px-3 text-sm"
        : "h-9 px-4 text-sm",
  ),
);
const inactive = computed(() => props.disabled || props.loading);
</script>

<template>
  <Primitive
    :as-child="asChild"
    :as="asChild ? undefined : 'button'"
    :type="asChild ? undefined : type"
    :disabled="asChild ? undefined : inactive"
    :aria-disabled="asChild && inactive ? true : undefined"
    :aria-busy="loading || undefined"
    :class="classes"
  >
    <Spinner v-if="loading" size="sm" />
    <slot />
  </Primitive>
</template>
