<script setup lang="ts">
import { RouterLink } from "vue-router";
import type { NavTabItem } from "./types";

/** Route-backed tabs (each tab is a page with its own URL). */
defineProps<{ items: NavTabItem[]; label: string }>();
</script>

<template>
  <nav :aria-label="label" class="border-b border-border">
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
</template>
