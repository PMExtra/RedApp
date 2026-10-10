<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { RouterLink, type RouteLocationRaw } from "vue-router";
import type { Schema } from "@/shared/api";
import { useFormat, useLocalized } from "@/shared/i18n";
import { cn } from "@/shared/ui";

/**
 * Category chips of the full catalog. Counts are site-wide (independent of
 * the search); choosing a chip keeps the search text and restarts paging.
 */
const props = defineProps<{
  categories: Schema<"CategoryCount">[];
  /** Selected category ID; empty for all categories. */
  current: string;
  q: string;
}>();
const { t } = useI18n();
const localized = useLocalized();
const format = useFormat();

function link(category: string): RouteLocationRaw {
  return {
    path: "/all",
    query: { ...(props.q ? { q: props.q } : {}), ...(category ? { category } : {}) },
  };
}
const chip = (active: boolean) =>
  cn(
    "inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-sm transition-colors focus-ring",
    active
      ? "border-primary bg-primary text-primary-fg"
      : "border-border bg-surface text-fg hover:bg-surface-hover",
  );
</script>

<template>
  <nav :aria-label="t('catalog.categories.label')">
    <ul class="flex flex-wrap gap-2">
      <li>
        <RouterLink
          :to="link('')"
          :class="chip(!current)"
          :aria-current="current ? undefined : 'page'"
        >
          {{ t("catalog.categories.all") }}
        </RouterLink>
      </li>
      <li v-for="item in categories" :key="item.id">
        <RouterLink
          :to="link(item.id)"
          :class="chip(current === item.id)"
          :aria-current="current === item.id ? 'page' : undefined"
          :aria-label="
            t('catalog.categories.chip', {
              name: localized(item.name) || item.id,
              count: format.number(item.count),
            })
          "
        >
          {{ localized(item.name) || item.id }}
          <span class="text-xs tabular-nums opacity-80">{{ format.number(item.count) }}</span>
        </RouterLink>
      </li>
    </ul>
  </nav>
</template>
