<script setup lang="ts">
import { computed } from "vue";
import { CircleAlert, CircleCheck, Info, TriangleAlert } from "@lucide/vue";
import { cn } from "./cn";

const props = withDefaults(
  defineProps<{ tone?: "info" | "success" | "warning" | "danger"; title?: string }>(),
  { tone: "info" },
);
defineSlots<{ default?: () => unknown; actions?: () => unknown }>();
const styles = {
  info: ["border-info/30 bg-info-soft", "text-info", Info],
  success: ["border-success/30 bg-success-soft", "text-success", CircleCheck],
  warning: ["border-warning/30 bg-warning-soft", "text-warning", TriangleAlert],
  danger: ["border-danger/30 bg-danger-soft", "text-danger", CircleAlert],
} as const;
const style = computed(() => styles[props.tone]);
</script>

<template>
  <div
    :role="tone === 'danger' || tone === 'warning' ? 'alert' : 'status'"
    :class="cn('flex gap-3 rounded-lg border p-3 text-sm', style[0])"
  >
    <component :is="style[2]" :class="cn('mt-0.5 size-4 shrink-0', style[1])" aria-hidden="true" />
    <div class="flex min-w-0 flex-1 flex-col gap-1">
      <p v-if="title" class="font-medium">{{ title }}</p>
      <div class="text-fg"><slot /></div>
      <div v-if="$slots.actions" class="mt-1 flex flex-wrap gap-2"><slot name="actions" /></div>
    </div>
  </div>
</template>
