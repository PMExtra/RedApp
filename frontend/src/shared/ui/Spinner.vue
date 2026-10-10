<script setup lang="ts">
import { LoaderCircle } from "@lucide/vue";

withDefaults(defineProps<{ size?: "sm" | "md" | "lg"; label?: string }>(), { size: "md" });
</script>

<template>
  <span class="inline-flex items-center" :role="label ? 'status' : undefined">
    <LoaderCircle
      aria-hidden="true"
      class="animate-spin"
      :class="{ 'size-4': size === 'sm', 'size-5': size === 'md', 'size-8': size === 'lg' }"
    />
    <span v-if="label" class="sr-only">{{ label }}</span>
  </span>
</template>
