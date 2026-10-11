<script setup lang="ts">
import { RouterLink } from "vue-router";
import type { NavTabItem } from "./types";

/**
 * Route-backed tabs (each tab is a page with its own URL). The optional `end`
 * slot holds controls for the whole page at the row's end; on narrow screens
 * it wraps above the tabs so the tabs stay on the border.
 */
defineProps<{ items: NavTabItem[]; label: string }>();
defineSlots<{ end?: () => unknown }>();
</script>

<template>
  <!-- Under wrap-reverse, items-start (flex-start) aligns to the bottom border. -->
  <div
    class="flex flex-wrap-reverse items-start justify-between gap-x-6 gap-y-2 border-b border-border"
  >
    <nav :aria-label="label" class="max-w-full min-w-0">
      <ul class="-mb-px flex gap-1 overflow-x-auto">
        <li v-for="item in items" :key="item.label">
          <RouterLink
            :to="item.to"
            class="inline-block border-b-2 border-transparent px-3 py-2 text-sm font-medium whitespace-nowrap text-muted hover:text-fg focus-ring"
            exact-active-class="!border-primary !text-fg"
          >
            {{ item.label }}
          </RouterLink>
        </li>
      </ul>
    </nav>
    <div v-if="$slots.end" class="flex flex-wrap items-center gap-x-4 gap-y-2 pb-1.5">
      <slot name="end" />
    </div>
  </div>
</template>
