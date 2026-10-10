<script setup lang="ts" generic="Row">
import { ArrowDown, ArrowUp, ArrowUpDown } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { cn } from "./cn";
import AsyncState from "./AsyncState.vue";
import Skeleton from "./Skeleton.vue";
import type { DataTableColumn, DataTableSort } from "./types";

/**
 * Table with sortable headers and loading / error / empty states. Cells
 * default to `row[column.key]`; customize with `#cell-<key>="{ row }"`.
 * Sorting only emits the new `sort`; the caller refetches.
 */
const sort = defineModel<DataTableSort | null>("sort", { default: null });
const props = defineProps<{
  columns: DataTableColumn[];
  rows: Row[] | undefined;
  rowKey: (row: Row) => string;
  caption?: string;
  loading?: boolean;
  error?: unknown;
  emptyText?: string;
}>();
const emit = defineEmits<{ retry: [] }>();
defineSlots<
  Record<`cell-${string}`, (props: { row: Row }) => unknown> & {
    empty?: () => unknown;
  }
>();
const { t } = useI18n();

function toggle(column: DataTableColumn) {
  const current = sort.value;
  sort.value =
    current?.key === column.key
      ? { key: column.key, direction: current.direction === "asc" ? "desc" : "asc" }
      : { key: column.key, direction: "asc" };
}

function ariaSort(column: DataTableColumn) {
  if (!column.sortable) return undefined;
  if (sort.value?.key !== column.key) return "none";
  return sort.value.direction === "asc" ? "ascending" : "descending";
}

function cellValue(row: Row, key: string): unknown {
  return typeof row === "object" && row !== null ? (row as Record<string, unknown>)[key] : "";
}

const alignClass = { start: "text-start", end: "text-end", center: "text-center" };
</script>

<template>
  <AsyncState
    :loading="false"
    :error="props.rows === undefined ? props.error : undefined"
    @retry="emit('retry')"
  >
    <div class="overflow-x-auto rounded-lg border border-border bg-surface">
      <table class="w-full border-collapse text-sm" :aria-busy="loading || undefined">
        <caption v-if="caption" class="sr-only">
          {{
            caption
          }}
        </caption>
        <thead class="bg-surface-sunken text-xs text-muted">
          <tr>
            <th
              v-for="column in columns"
              :key="column.key"
              scope="col"
              :aria-sort="ariaSort(column)"
              :class="
                cn('px-3 py-2 font-medium', alignClass[column.align ?? 'start'], column.class)
              "
            >
              <button
                v-if="column.sortable"
                type="button"
                class="inline-flex items-center gap-1 rounded-sm hover:text-fg focus-ring"
                @click="toggle(column)"
              >
                <span :class="column.hideLabel && 'sr-only'">{{ column.label }}</span>
                <ArrowUp
                  v-if="sort?.key === column.key && sort.direction === 'asc'"
                  class="size-3.5"
                  aria-hidden="true"
                />
                <ArrowDown
                  v-else-if="sort?.key === column.key"
                  class="size-3.5"
                  aria-hidden="true"
                />
                <ArrowUpDown v-else class="size-3.5 opacity-50" aria-hidden="true" />
              </button>
              <span v-else :class="column.hideLabel && 'sr-only'">{{ column.label }}</span>
            </th>
          </tr>
        </thead>
        <tbody>
          <template v-if="rows === undefined && loading">
            <tr v-for="index in 5" :key="index" class="border-t border-border">
              <td v-for="column in columns" :key="column.key" class="px-3 py-2.5">
                <Skeleton class="h-4 w-3/4" />
              </td>
            </tr>
          </template>
          <tr v-else-if="!rows?.length">
            <td :colspan="columns.length" class="px-3 py-10 text-center text-muted">
              <slot name="empty">{{ emptyText ?? t("ui.table.empty") }}</slot>
            </td>
          </tr>
          <tr
            v-for="row in rows"
            v-else
            :key="rowKey(row)"
            class="border-t border-border hover:bg-surface-hover"
          >
            <td
              v-for="column in columns"
              :key="column.key"
              :class="
                cn('px-3 py-2.5 align-middle', alignClass[column.align ?? 'start'], column.class)
              "
            >
              <slot :name="`cell-${column.key}`" :row="row">{{ cellValue(row, column.key) }}</slot>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </AsyncState>
</template>
