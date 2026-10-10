<script setup lang="ts">
import { computed, ref, useId, watch } from "vue";
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
import Button from "./Button.vue";
import Input from "./Input.vue";

/**
 * Page-style pagination (`page`/`limit` → `total`, `total_pages`). Beyond
 * `JUMP_AFTER` pages a "go to page" box joins the links. Emits page changes
 * only; the caller loads the page.
 */
const page = defineModel<number>("page", { required: true });
const props = defineProps<{ total: number; pageSize: number }>();
const { t } = useI18n();
const id = useId();
const JUMP_AFTER = 7;
const totalPages = computed(() => Math.max(1, Math.ceil(props.total / props.pageSize)));
const requested = ref("");
const invalid = ref(false);

watch(page, () => {
  requested.value = "";
  invalid.value = false;
});

function jump() {
  const value = requested.value.trim();
  const next = Number(value);
  invalid.value = !/^\d+$/.test(value) || next < 1 || next > totalPages.value;
  if (invalid.value) return;
  requested.value = "";
  page.value = next;
}
const button =
  "inline-flex size-8 items-center justify-center rounded-md text-sm hover:bg-surface-hover focus-ring disabled:pointer-events-none disabled:opacity-40";
</script>

<template>
  <div v-if="totalPages > 1" class="flex flex-wrap items-center justify-between gap-3">
    <PaginationRoot
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
    <!-- Not a <form>: lists with pagination may sit inside one. -->
    <div v-if="totalPages > JUMP_AFTER" class="flex flex-col gap-1">
      <div class="flex items-center gap-2">
        <label :for="`${id}-page`" class="text-sm text-muted">
          {{ t("ui.pagination.jump.label") }}
        </label>
        <Input
          :id="`${id}-page`"
          v-model="requested"
          class="w-20"
          inputmode="numeric"
          :placeholder="String(page)"
          :aria-invalid="invalid ? 'true' : undefined"
          :aria-describedby="invalid ? `${id}-error` : undefined"
          @input="invalid = false"
          @keydown.enter.prevent="jump"
        />
        <Button size="sm" @click="jump">{{ t("ui.pagination.jump.go") }}</Button>
      </div>
      <p v-if="invalid" :id="`${id}-error`" role="alert" class="text-xs text-danger">
        {{ t("ui.pagination.jump.invalid", { total: totalPages }) }}
      </p>
    </div>
  </div>
</template>
