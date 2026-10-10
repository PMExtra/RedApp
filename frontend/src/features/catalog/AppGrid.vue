<script setup lang="ts">
import type { Schema } from "@/shared/api";
import { Skeleton } from "@/shared/ui";
import AppCard from "./AppCard.vue";

/** Responsive grid of application cards; `loading` shows placeholder tiles. */
withDefaults(
  defineProps<{
    apps?: Schema<"PublicApp">[];
    loading?: boolean;
    placeholders?: number;
    headingLevel?: 2 | 3;
  }>(),
  { apps: () => [], placeholders: 6, headingLevel: 3 },
);
const grid = "grid gap-4 sm:grid-cols-2 lg:grid-cols-3";
</script>

<template>
  <div v-if="loading" :class="grid">
    <div
      v-for="index in placeholders"
      :key="index"
      class="flex flex-col gap-3 rounded-xl border border-border bg-surface p-4"
    >
      <div class="flex items-center gap-3">
        <Skeleton class="size-12 rounded-lg" />
        <div class="flex flex-1 flex-col gap-2">
          <Skeleton class="h-4 w-1/2" />
          <Skeleton class="h-3 w-1/3" />
        </div>
      </div>
      <Skeleton class="h-3 w-full" />
      <Skeleton class="h-3 w-4/5" />
    </div>
  </div>
  <ul v-else :class="grid">
    <li v-for="app in apps" :key="app.key">
      <AppCard :app="app" :heading-level="headingLevel" />
    </li>
  </ul>
</template>
