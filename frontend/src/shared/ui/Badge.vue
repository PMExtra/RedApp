<script setup lang="ts">
import type { BadgeTone } from "./types";

withDefaults(defineProps<{ tone?: BadgeTone }>(), { tone: "neutral" });
const tones: Record<BadgeTone, string> = {
  neutral: "bg-surface-sunken text-muted border-border",
  primary: "bg-primary-soft text-primary border-primary/20",
  success: "bg-success-soft text-success border-success/20",
  warning: "bg-warning-soft text-warning border-warning/20",
  danger: "bg-danger-soft text-danger border-danger/20",
  info: "bg-info-soft text-info border-info/20",
};
</script>

<template>
  <span
    class="inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-medium whitespace-nowrap [&_svg]:size-3"
    :class="tones[tone]"
  >
    <slot />
  </span>
</template>
