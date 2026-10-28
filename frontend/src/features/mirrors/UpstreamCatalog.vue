<script setup lang="ts">
import { computed, ref } from "vue";
import { Copy, Search } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import { useListQuery, useProviders } from "@/features/directory";
import { useLocalized } from "@/shared/i18n";
import {
  Badge,
  Button,
  Card,
  Checkbox,
  DataTable,
  Field,
  Input,
  Pagination,
  Tooltip,
  type DataTableColumn,
} from "@/shared/ui";
import MirrorDialog from "./MirrorDialog.vue";
import {
  UPSTREAM_CATALOG_PAGE_SIZE,
  useUpstreamCatalog,
  type UpstreamCatalogItem,
} from "./queries";
import { splitKey } from "./target";

/**
 * The applications an upstream publishes, with search and pages kept in the
 * URL (`?q=&page=`). Supported applications that are not mirrored yet can be
 * selected (across pages) and mirrored together. Upstream icons are not shown:
 * the admin pages only load same-origin images.
 */
const props = defineProps<{ upstream: string }>();
const { t } = useI18n();
const localized = useLocalized();
const providers = useProviders();
const list = useListQuery();
const catalog = useUpstreamCatalog(
  () => props.upstream,
  () => ({ q: list.q.value, page: list.page.value }),
);

const selected = ref(new Map<string, UpstreamCatalogItem>());
const mirroring = ref(false);
const selection = computed(() => [...selected.value.values()]);

const columns = computed<DataTableColumn[]>(() => [
  { key: "select", label: t("mirrors.catalog.select"), hideLabel: true, class: "w-10" },
  { key: "name", label: t("mirrors.catalog.name") },
  { key: "key", label: t("mirrors.catalog.key") },
  { key: "provider", label: t("mirrors.catalog.provider") },
  { key: "status", label: t("mirrors.catalog.status") },
]);

function selectable(item: UpstreamCatalogItem): boolean {
  return item.supported && item.mirror === null;
}

function toggle(item: UpstreamCatalogItem, checked: boolean | "indeterminate"): void {
  const next = new Map(selected.value);
  if (checked === true) next.set(item.key, item);
  else next.delete(item.key);
  selected.value = next;
}

function providerName(key: string): string {
  const provider = providers.data.value?.items.find((item) => item.key === key);
  return provider ? localized(provider.name) || key : key;
}

function nameOf(item: UpstreamCatalogItem): string {
  return localized(item.name) || item.key;
}

function mirrored(keys: string[]): void {
  const next = new Map(selected.value);
  for (const key of keys) next.delete(key);
  selected.value = next;
}
</script>

<template>
  <Card :title="t('mirrors.catalog.title')" :description="t('mirrors.catalog.description')">
    <div class="flex flex-col gap-4">
      <div class="flex flex-wrap items-end gap-3">
        <div class="relative w-full max-w-sm">
          <Field v-slot="{ control }" :label="t('mirrors.catalog.search')" hide-label>
            <Search
              class="pointer-events-none absolute start-2.5 top-2.5 size-4 text-muted"
              aria-hidden="true"
            />
            <Input
              v-bind="control"
              v-model="list.search.value"
              type="search"
              maxlength="128"
              class="ps-8"
              :placeholder="t('mirrors.catalog.search')"
            />
          </Field>
        </div>
        <div class="ms-auto flex flex-wrap items-center gap-2">
          <span v-if="selection.length > 0" class="text-sm text-muted" role="status">
            {{ t("mirrors.catalog.selected", { count: selection.length }) }}
          </span>
          <Button v-if="selection.length > 0" variant="ghost" @click="selected = new Map()">
            {{ t("mirrors.catalog.clear") }}
          </Button>
          <Button variant="primary" :disabled="selection.length === 0" @click="mirroring = true">
            <Copy aria-hidden="true" /> {{ t("mirrors.catalog.mirrorSelected") }}
          </Button>
        </div>
      </div>

      <DataTable
        :caption="t('mirrors.catalog.caption')"
        :columns="columns"
        :rows="catalog.data.value?.items"
        :row-key="(row: UpstreamCatalogItem) => row.key"
        :loading="catalog.isFetching.value"
        :error="catalog.error.value"
        :empty-text="list.q.value ? t('mirrors.catalog.noMatches') : t('mirrors.catalog.empty')"
        @retry="catalog.refetch()"
      >
        <template #cell-select="{ row }">
          <Checkbox
            v-if="selectable(row)"
            :model-value="selected.has(row.key)"
            :aria-label="t('mirrors.catalog.selectApp', { name: nameOf(row) })"
            @update:model-value="toggle(row, $event)"
          />
        </template>
        <template #cell-name="{ row }">
          <span class="flex flex-col">
            <span class="font-medium">{{ nameOf(row) }}</span>
            <span v-if="localized(row.description)" class="line-clamp-1 text-xs text-muted">
              {{ localized(row.description) }}
            </span>
          </span>
        </template>
        <template #cell-key="{ row }">
          <span class="font-mono text-xs">{{ row.key }}</span>
        </template>
        <template #cell-provider="{ row }">{{ providerName(row.provider) }}</template>
        <template #cell-status="{ row }">
          <RouterLink
            v-if="row.mirror"
            :to="{ name: 'admin-app-settings', params: splitKey(row.mirror) }"
            class="rounded-sm text-sm text-primary hover:underline focus-ring"
          >
            {{ t("mirrors.catalog.mirroredAs", { key: row.mirror }) }}
          </RouterLink>
          <Tooltip v-else-if="!row.supported" :content="t('mirrors.catalog.unsupportedHint')">
            <Badge tone="warning" tabindex="0">{{ t("mirrors.catalog.unsupported") }}</Badge>
          </Tooltip>
          <span v-else class="text-sm text-muted">{{ t("mirrors.catalog.available") }}</span>
        </template>
      </DataTable>

      <Pagination
        v-if="catalog.data.value"
        :page="list.page.value"
        :total="catalog.data.value.total"
        :page-size="UPSTREAM_CATALOG_PAGE_SIZE"
        @update:page="list.setPage"
      />
    </div>
    <MirrorDialog
      v-model:open="mirroring"
      :upstream="upstream"
      :items="selection"
      @mirrored="mirrored"
    />
  </Card>
</template>
