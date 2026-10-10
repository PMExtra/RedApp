<script setup lang="ts">
import { computed, nextTick, ref, useTemplateRef, watch } from "vue";
import { Search } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import {
  AppGrid,
  CATALOG_PAGE_SIZE,
  CategoryFilter,
  PagedNavigation,
  parsePage,
  useCatalog,
  usePublicVendor,
  vendorLogo,
} from "@/features/catalog";
import { isApiError } from "@/shared/api";
import { useLocalized, type Locale } from "@/shared/i18n";
import { isSlug, useDebounced, useDocumentTitle } from "@/shared/lib";
import { AsyncState, Button, EntityIcon, Input, PageHeader, Skeleton } from "@/shared/ui";
import NotFoundPage from "./NotFoundPage.vue";

/**
 * `/all` and `/{vendor}`. The URL holds the state (`q`, `category`, `page`),
 * so reloads, shared links and the browser's Back button restore it. Typing
 * replaces the URL after 250 ms; categories and pages push a new entry.
 */
const SEARCH_DELAY_MS = 250;
const { t, locale } = useI18n();
const localized = useLocalized();
const route = useRoute();
const router = useRouter();

const vendor = computed(() => (typeof route.params.vendor === "string" ? route.params.vendor : ""));
const q = computed(() =>
  typeof route.query.q === "string" ? route.query.q.trim().slice(0, 128) : "",
);
// Categories filter the full catalog only.
const category = computed(() => {
  const value = route.query.category;
  return !vendor.value && typeof value === "string" && isSlug(value) ? value : "";
});
const page = computed(() => parsePage(route.query.page));

function location(next: { q?: string; category?: string; page?: number }) {
  const query: Record<string, string> = {};
  if (next.q) query.q = next.q;
  if (next.category) query.category = next.category;
  if (next.page && next.page > 1) query.page = String(next.page);
  return { path: route.path, query };
}

// The search box follows the URL (Back, header search) and writes it back
// once typing pauses.
const search = ref(q.value);
watch(q, (value) => {
  if (value !== search.value.trim()) search.value = value;
});
const typed = useDebounced(search, SEARCH_DELAY_MS);
watch(typed, (value) => {
  applySearch(value);
});
function applySearch(value: string) {
  const next = value.trim();
  if (next === q.value) return;
  void router.replace(location({ q: next, category: category.value }));
}
function clearSearch() {
  search.value = "";
  applySearch("");
}

const resultsHeading = useTemplateRef<HTMLElement>("results");
const currentPage = computed({
  get: () => page.value,
  set: (next: number) => {
    void router.push(location({ q: q.value, category: category.value, page: next })).then(() =>
      nextTick(() => resultsHeading.value?.focus()),
    );
  },
});

const vendorQuery = usePublicVendor(vendor);
const catalog = useCatalog(() => ({
  q: q.value,
  vendor: vendor.value,
  category: category.value,
  page: page.value,
}));
const data = computed(() => catalog.data.value);

const notFound = computed(() =>
  [vendorQuery.error.value, catalog.error.value].some(
    (error) => isApiError(error) && error.code === "VENDOR_NOT_FOUND",
  ),
);
const vendorData = computed(() => vendorQuery.data.value);
const vendorName = computed(() =>
  vendorData.value ? localized(vendorData.value.name) || vendorData.value.id : "",
);
const logo = computed(() =>
  vendorData.value ? vendorLogo(vendorData.value, locale.value as Locale) : "",
);
const title = computed(() => (vendor.value ? vendorName.value : t("catalog.list.title")));
useDocumentTitle(() =>
  notFound.value ? t("publicShell.titles.notFound") : vendor.value ? vendorName.value : null,
);

const error = computed(() => (vendor.value ? vendorQuery.error.value : null) ?? catalog.error.value);
function retry() {
  if (vendorQuery.isError.value) void vendorQuery.refetch();
  void catalog.refetch();
}

const emptyText = computed(() => {
  if (q.value) return t("catalog.list.noMatches", { q: q.value });
  return vendor.value ? t("catalog.list.vendorEmpty") : t("catalog.list.empty");
});
</script>

<template>
  <NotFoundPage v-if="notFound" />
  <div v-else class="flex flex-col gap-6">
    <div v-if="vendor && vendorQuery.isPending.value" class="flex flex-col gap-3" aria-hidden="true">
      <Skeleton class="h-4 w-40" />
      <Skeleton class="h-8 w-64" />
      <Skeleton class="h-4 w-96 max-w-full" />
    </div>
    <PageHeader
      v-else
      :title="title || vendor"
      :description="vendorData ? localized(vendorData.description) : undefined"
      :breadcrumbs="
        vendor ? [{ label: t('catalog.list.title'), to: '/all' }, { label: title || vendor }] : []
      "
    >
      <template v-if="logo" #actions>
        <EntityIcon :src="logo" variant="logo" size="lg" />
      </template>
    </PageHeader>

    <div class="flex flex-col gap-4">
      <form role="search" class="relative max-w-xl" @submit.prevent="applySearch(search)">
        <Search
          class="pointer-events-none absolute start-3 top-1/2 size-4 -translate-y-1/2 text-subtle"
          aria-hidden="true"
        />
        <Input
          v-model="search"
          type="search"
          maxlength="128"
          class="ps-9"
          :aria-label="t('catalog.list.search')"
          :placeholder="t('catalog.list.placeholder')"
        />
      </form>
      <CategoryFilter
        v-if="!vendor && data && (data.categories.length || category)"
        :categories="data.categories"
        :current="category"
        :q="q"
      />
    </div>

    <section class="flex flex-col gap-4" aria-labelledby="catalog-results">
      <h2 id="catalog-results" ref="results" tabindex="-1" class="sr-only">
        {{ t("catalog.home.title") }}
      </h2>
      <p v-if="data" role="status" class="text-sm text-muted">
        {{ t("catalog.list.results", data.total) }}
      </p>
      <AsyncState
        :loading="catalog.isPending.value && !error"
        :error="error"
        :empty="data?.items.length === 0"
        @retry="retry"
      >
        <template #loading>
          <AppGrid loading />
        </template>
        <template #empty>
          <div class="flex flex-col items-center gap-3">
            <template v-if="data && data.total > 0">
              <p>{{ t("catalog.list.pageEmpty") }}</p>
              <Button size="sm" @click="currentPage = 1">{{ t("catalog.list.firstPage") }}</Button>
            </template>
            <template v-else>
              <p>{{ emptyText }}</p>
              <Button v-if="q" size="sm" @click="clearSearch">
                {{ t("catalog.list.clearSearch") }}
              </Button>
            </template>
          </div>
        </template>
        <AppGrid
          :apps="data?.items"
          :aria-busy="catalog.isPlaceholderData.value || undefined"
        />
      </AsyncState>
      <PagedNavigation
        v-if="data && data.items.length"
        v-model:page="currentPage"
        :total="data.total"
        :page-size="CATALOG_PAGE_SIZE"
      />
    </section>
  </div>
</template>
