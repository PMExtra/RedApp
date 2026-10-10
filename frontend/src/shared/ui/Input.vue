<script setup lang="ts">
import { cn, controlClass } from "./cn";

defineOptions({ inheritAttrs: false });
const model = defineModel<string>({ default: "" });
defineProps<{ class?: string }>();
</script>

<template>
  <input v-bind="$attrs" v-model="model" :class="cn(controlClass, 'h-9', $props.class)" />
</template>
