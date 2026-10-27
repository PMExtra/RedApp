<script setup lang="ts">
/** Public search stays mounted while only its parent grid changes. */
withDefaults(defineProps<{ publicLayout?: boolean; customNavigation?: boolean }>(), {
  publicLayout: false,
  customNavigation: false,
});
defineSlots<{
  brand?: () => unknown;
  navigation?: () => unknown;
  default?: () => unknown;
  actions?: () => unknown;
}>();
</script>

<template>
  <header class="sticky top-0 z-sticky border-b border-border bg-surface/90 backdrop-blur">
    <div
      :class="[
        'mx-auto max-w-7xl items-center px-4 sm:px-6',
        publicLayout
          ? [
              'grid grid-cols-[minmax(0,1fr)_auto] gap-x-3 gap-y-2 py-2 lg:h-14 lg:py-0',
              customNavigation
                ? 'lg:grid-cols-[minmax(0,1fr)_minmax(8rem,20rem)_auto]'
                : 'lg:grid-cols-[minmax(0,1fr)_minmax(0,28rem)_minmax(0,1fr)]',
            ]
          : [
              'h-14 gap-4',
              $slots.default ? 'grid grid-cols-[1fr_minmax(0,28rem)_1fr]' : 'flex justify-between',
            ],
      ]"
    >
      <div
        :class="
          publicLayout ? 'flex min-w-0 items-center gap-2' : 'flex shrink-0 items-center gap-2'
        "
      >
        <slot name="brand" />
        <div v-if="$slots.navigation" class="min-w-0 shrink-0"><slot name="navigation" /></div>
      </div>
      <div
        v-if="$slots.default"
        :class="[
          'min-w-0',
          publicLayout && 'col-span-2 row-start-2 lg:col-span-1 lg:col-start-2 lg:row-start-1',
        ]"
      >
        <slot />
      </div>
      <div class="flex shrink-0 items-center justify-end gap-1"><slot name="actions" /></div>
    </div>
  </header>
</template>
