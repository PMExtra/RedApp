<script setup lang="ts">
import Breadcrumbs from "./Breadcrumbs.vue";
import type { Crumb } from "./types";

/** The page's h1 with optional breadcrumbs, description, media and actions. */
defineProps<{ title: string; description?: string; breadcrumbs?: Crumb[] }>();
defineSlots<{
  media?: () => unknown;
  meta?: () => unknown;
  actions?: () => unknown;
}>();
</script>

<template>
  <div class="flex flex-col gap-3">
    <Breadcrumbs v-if="breadcrumbs?.length" :items="breadcrumbs" />
    <div class="flex flex-wrap items-start justify-between gap-4">
      <div class="flex min-w-0 items-start gap-4">
        <slot name="media" />
        <div class="flex min-w-0 flex-col gap-1">
          <h1 class="text-2xl font-semibold tracking-tight break-words">{{ title }}</h1>
          <p v-if="description" class="max-w-3xl text-sm text-muted">{{ description }}</p>
          <div v-if="$slots.meta" class="flex flex-wrap items-center gap-2 text-sm text-muted">
            <slot name="meta" />
          </div>
        </div>
      </div>
      <div v-if="$slots.actions" class="flex flex-wrap items-center gap-2">
        <slot name="actions" />
      </div>
    </div>
  </div>
</template>
