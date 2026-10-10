<script setup lang="ts">
withDefaults(
  defineProps<{
    title?: string;
    description?: string;
    /** Heading level of the title, to fit the page outline. */
    level?: 2 | 3 | 4;
    as?: string;
  }>(),
  { level: 2, as: "section" },
);
defineSlots<{ default?: () => unknown; actions?: () => unknown; footer?: () => unknown }>();
</script>

<template>
  <component :is="as" class="rounded-xl border border-border bg-surface shadow-sm">
    <div
      v-if="title || $slots.actions"
      class="flex flex-wrap items-start justify-between gap-3 border-b border-border px-5 py-4"
    >
      <div class="flex flex-col gap-1">
        <component :is="`h${level}`" v-if="title" class="text-base font-semibold">
          {{ title }}
        </component>
        <p v-if="description" class="text-sm text-muted">{{ description }}</p>
      </div>
      <div v-if="$slots.actions" class="flex flex-wrap gap-2"><slot name="actions" /></div>
    </div>
    <div class="px-5 py-4"><slot /></div>
    <div
      v-if="$slots.footer"
      class="flex flex-wrap justify-end gap-2 border-t border-border px-5 py-3"
    >
      <slot name="footer" />
    </div>
  </component>
</template>
