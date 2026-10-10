<script setup lang="ts">
import { cn, controlClass } from "./cn";

defineOptions({ inheritAttrs: false });
const model = defineModel<string>({ default: "" });
withDefaults(defineProps<{ rows?: number; monospace?: boolean; class?: string }>(), { rows: 4 });
</script>

<template>
  <textarea
    v-bind="$attrs"
    v-model="model"
    :rows="rows"
    :class="
      cn(controlClass, 'min-h-16 py-2 leading-relaxed', monospace && 'font-mono', $props.class)
    "
  />
</template>
