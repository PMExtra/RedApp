<script setup lang="ts">
/**
 * Sticky header bar: brand on the start side, actions on the end side and the
 * optional default slot (the public search) centered between them.
 */
defineSlots<{ brand?: () => unknown; default?: () => unknown; actions?: () => unknown }>();
</script>

<template>
  <header class="sticky top-0 z-sticky border-b border-border bg-surface/90 backdrop-blur">
    <div
      :class="[
        'mx-auto h-14 max-w-7xl items-center gap-4 px-4 sm:px-6',
        $slots.default ? 'grid grid-cols-[1fr_minmax(0,28rem)_1fr]' : 'flex justify-between',
      ]"
    >
      <div class="flex shrink-0 items-center gap-2"><slot name="brand" /></div>
      <div v-if="$slots.default" class="min-w-0"><slot /></div>
      <div class="flex shrink-0 items-center justify-end gap-1"><slot name="actions" /></div>
    </div>
  </header>
</template>
