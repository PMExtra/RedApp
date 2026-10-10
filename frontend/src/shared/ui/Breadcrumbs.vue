<script setup lang="ts">
import { ChevronRight } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import type { Crumb } from "./types";

defineProps<{ items: Crumb[] }>();
const { t } = useI18n();
</script>

<template>
  <nav :aria-label="t('ui.nav.breadcrumbs')">
    <ol class="flex flex-wrap items-center gap-1 text-sm text-muted">
      <li v-for="(item, index) in items" :key="index" class="flex items-center gap-1">
        <ChevronRight v-if="index > 0" class="size-3.5 text-subtle" aria-hidden="true" />
        <RouterLink
          v-if="item.to && index < items.length - 1"
          :to="item.to"
          class="rounded-sm hover:text-fg hover:underline focus-ring"
        >
          {{ item.label }}
        </RouterLink>
        <span
          v-else
          :aria-current="index === items.length - 1 ? 'page' : undefined"
          class="text-fg"
        >
          {{ item.label }}
        </span>
      </li>
    </ol>
  </nav>
</template>
