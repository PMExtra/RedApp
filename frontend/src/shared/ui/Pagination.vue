<script setup lang="ts">
import { computed } from "vue";
import { ChevronLeft, ChevronRight, ChevronsLeft, ChevronsRight } from "@lucide/vue";
import {
  PaginationEllipsis,
  PaginationFirst,
  PaginationLast,
  PaginationList,
  PaginationListItem,
  PaginationNext,
  PaginationPrev,
  PaginationRoot,
} from "reka-ui";
import { useI18n } from "vue-i18n";

/** Page-style pagination (`page`/`limit` → `total`, `total_pages`). */
const page = defineModel<number>("page", { required: true });
const props = defineProps<{ total: number; pageSize: number }>();
const { t } = useI18n();
const totalPages = computed(() => Math.max(1, Math.ceil(props.total / props.pageSize)));
const button =
  "inline-flex size-8 items-center justify-center rounded-md text-sm hover:bg-surface-hover focus-ring disabled:pointer-events-none disabled:opacity-40";
</script>

<template>
  <PaginationRoot
    v-if="totalPages > 1"
    v-model:page="page"
    :total="total"
    :items-per-page="pageSize"
    :sibling-count="1"
    show-edges
    :aria-label="t('ui.pagination.label')"
    as="nav"
  >
    <PaginationList v-slot="{ items }" class="flex flex-wrap items-center gap-1">
      <PaginationFirst :class="button" :aria-label="t('ui.pagination.first')">
        <ChevronsLeft class="size-4" aria-hidden="true" />
      </PaginationFirst>
      <PaginationPrev :class="button" :aria-label="t('ui.pagination.previous')">
        <ChevronLeft class="size-4" aria-hidden="true" />
      </PaginationPrev>
      <template v-for="(item, index) in items" :key="index">
        <PaginationListItem
          v-if="item.type === 'page'"
          :value="item.value"
          :aria-label="t('ui.pagination.page', { page: item.value })"
          :class="button"
          class="tabular-nums data-[selected]:bg-primary data-[selected]:text-primary-fg"
        >
          {{ item.value }}
        </PaginationListItem>
        <PaginationEllipsis v-else :index="index" class="px-1 text-muted">…</PaginationEllipsis>
      </template>
      <PaginationNext :class="button" :aria-label="t('ui.pagination.next')">
        <ChevronRight class="size-4" aria-hidden="true" />
      </PaginationNext>
      <PaginationLast :class="button" :aria-label="t('ui.pagination.last')">
        <ChevronsRight class="size-4" aria-hidden="true" />
      </PaginationLast>
      <span class="ms-2 text-xs text-muted">
        {{ t("ui.pagination.pageOf", { page, total: totalPages }) }}
      </span>
    </PaginationList>
  </PaginationRoot>
</template>
