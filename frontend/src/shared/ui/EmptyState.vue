<script setup lang="ts">
defineProps<{ title: string; description?: string }>();
defineSlots<{ icon?: () => unknown; action?: () => unknown }>();
</script>

<template>
  <div class="flex flex-col items-center gap-2 px-4 py-10 text-center">
    <div v-if="$slots.icon" class="text-subtle [&_svg]:size-8"><slot name="icon" /></div>
    <p class="text-base font-medium">{{ title }}</p>
    <p v-if="description" class="max-w-md text-sm text-muted">{{ description }}</p>
    <div v-if="$slots.action" class="mt-2"><slot name="action" /></div>
  </div>
</template>
