<script setup lang="ts">
import { computed, ref } from "vue";
import { Pencil, Search } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { useListQuery } from "@/features/directory";
import {
  CATEGORY_PAGE_SIZE,
  CategoryEditDialog,
  useCategoryList,
  type Category,
} from "@/features/taxonomy";
import { useFormat, useLocalized, type Locale } from "@/shared/i18n";
import {
  Badge,
  Button,
  DataTable,
  Field,
  Input,
  PageHeader,
  Pagination,
  type DataTableColumn,
} from "@/shared/ui";

const { t, locale } = useI18n();
const localized = useLocalized();
const format = useFormat();
const list = useListQuery();
const categories = useCategoryList(() => ({ q: list.q.value, page: list.page.value }));
const editing = ref("");
const editOpen = ref(false);

const otherLanguage = computed<Locale>(() => (locale.value === "zh-CN" ? "en" : "zh-CN"));
const columns = computed<DataTableColumn[]>(() => [
  { key: "name", label: t("taxonomy.page.name") },
  { key: "other", label: t("taxonomy.page.otherName") },
  { key: "id", label: t("taxonomy.page.id") },
  { key: "applications", label: t("taxonomy.page.applications"), align: "end" },
  { key: "actions", label: t("taxonomy.page.actions"), hideLabel: true, align: "end" },
]);

function edit(category: Category): void {
  editing.value = category.id;
  editOpen.value = true;
}
</script>

<template>
  <div class="flex flex-col gap-6">
    <PageHeader
      :title="t('adminShell.titles.categories')"
      :description="t('taxonomy.page.description')"
    />
    <div class="relative w-full max-w-sm">
      <Field v-slot="{ control }" :label="t('taxonomy.page.search')" hide-label>
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
          :placeholder="t('taxonomy.page.search')"
        />
      </Field>
    </div>
    <DataTable
      :caption="t('taxonomy.page.caption')"
      :columns="columns"
      :rows="categories.data.value?.items"
      :row-key="(row: Category) => row.id"
      :loading="categories.isFetching.value"
      :error="categories.error.value"
      :empty-text="list.q.value ? t('taxonomy.page.noMatches') : t('taxonomy.page.empty')"
      @retry="categories.refetch()"
    >
      <template #cell-name="{ row }">
        <span class="flex items-center gap-2 font-medium">
          {{ localized(row.name) || row.id }}
          <Badge v-if="row.builtin">{{ t("taxonomy.page.builtin") }}</Badge>
        </span>
      </template>
      <template #cell-other="{ row }">
        <span class="text-muted" :lang="otherLanguage">{{ row.name[otherLanguage] }}</span>
      </template>
      <template #cell-id="{ row }">
        <span class="font-mono text-xs">{{ row.id }}</span>
      </template>
      <template #cell-applications="{ row }">
        <span class="tabular-nums">{{ format.number(row.applications) }}</span>
      </template>
      <template #cell-actions="{ row }">
        <Button
          size="sm"
          variant="ghost"
          :aria-label="t('taxonomy.page.rename', { name: localized(row.name) || row.id })"
          @click="edit(row)"
        >
          <Pencil aria-hidden="true" /> {{ t("common.actions.edit") }}
        </Button>
      </template>
    </DataTable>
    <Pagination
      v-if="categories.data.value"
      :page="list.page.value"
      :total="categories.data.value.total"
      :page-size="CATEGORY_PAGE_SIZE"
      @update:page="list.setPage"
    />
    <CategoryEditDialog v-model:open="editOpen" :category="editing" />
  </div>
</template>
