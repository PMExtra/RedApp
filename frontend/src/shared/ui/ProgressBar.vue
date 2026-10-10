<script setup lang="ts">
import { computed } from "vue";
import { ProgressIndicator, ProgressRoot } from "reka-ui";

/** Determinate when `value` is a number, indeterminate when `null`. */
const props = withDefaults(defineProps<{ value: number | null; max?: number; label: string }>(), {
  max: 100,
});
const percent = computed(() =>
  props.value === null ? 0 : Math.min(100, Math.max(0, (props.value / props.max) * 100)),
);
</script>

<template>
  <ProgressRoot
    :model-value="value"
    :max="max"
    :aria-label="label"
    class="relative h-2 w-full overflow-hidden rounded-full bg-surface-sunken"
  >
    <ProgressIndicator
      class="h-full bg-primary transition-[width]"
      :class="value === null && 'w-1/3 animate-pulse'"
      :style="value === null ? undefined : { width: `${percent}%` }"
    />
  </ProgressRoot>
</template>
