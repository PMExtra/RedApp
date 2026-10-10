<script setup lang="ts">
/** Sticky header bar: brand on the start side, navigation, actions on the end side. */
defineSlots<{ brand?: () => unknown; default?: () => unknown; actions?: () => unknown }>();
</script>

<template>
  <header class="sticky top-0 z-sticky border-b border-border bg-surface/90 backdrop-blur">
    <div class="mx-auto flex h-14 max-w-7xl items-center gap-4 px-4 sm:px-6">
      <div class="flex shrink-0 items-center gap-2"><slot name="brand" /></div>
      <div class="flex min-w-0 flex-1 items-center gap-2"><slot /></div>
      <div class="flex shrink-0 items-center gap-1"><slot name="actions" /></div>
    </div>
  </header>
</template>
