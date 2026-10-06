<script setup lang="ts">
import { vendorName } from "../vendorName";
import IconButton from "../components/IconButton.vue";
import { computed, ref, watch, onUnmounted } from "vue";
import { useRoute, useRouter } from "vue-router";
import type { Application } from "../bootstrap";
import type { LocalizedText } from "../site";
import type { NumberedPage } from "../composables/useNumberedCollection";
import { usePublicResource } from "../public";
import { language, t, errorText } from "../i18n";
import PageNavigation from "../components/PageNavigation.vue";
import PublicCards from "../components/PublicCards.vue";
const route = useRoute(),
  router = useRouter();
const vendor = computed(() => String(route.params.vendor || ""));
const query = computed(() =>
  typeof route.query.q === "string" ? route.query.q : "",
);
const page = computed(() =>
  /^[1-9]\d*$/.test(String(route.query.page)) ? Number(route.query.page) : 1,
);
const search = ref(query.value);
let timer: ReturnType<typeof setTimeout> | undefined;
watch(query, (value) => {
  clearTimeout(timer);
  search.value = value;
});
watch(search, (value) => {
  clearTimeout(timer);
  timer = setTimeout(() => {
    if (value.trim() !== query.value)
      void router.replace({ query: value.trim() ? { q: value.trim() } : {} });
  }, 250);
});
onUnmounted(() => clearTimeout(timer));
const path = computed(
  () =>
    `/api/catalog?${new URLSearchParams({ q: query.value, vendor: vendor.value, page: String(page.value), limit: "24" })}`,
);
const { data, error, loading, refresh } = usePublicResource<
  NumberedPage<Application>
>(
  path,
  (value) =>
    Array.isArray(value.items) &&
    Number.isSafeInteger(value.page) &&
    value.page > 0 &&
    Number.isSafeInteger(value.total_pages) &&
    value.total_pages >= value.page,
);
// Metadata shares the bounded request lifecycle, including vendor changes.
const vendorPath = computed(() =>
  vendor.value ? `/api/vendors/${vendor.value}` : "",
);
const vendorData = usePublicResource<{
  id: string;
  name: LocalizedText;
  description: LocalizedText;
  icon: string;
}>(vendorPath);
function go(next: number) {
  void router.push({
    query: {
      ...(query.value ? { q: query.value } : {}),
      ...(next > 1 ? { page: String(next) } : {}),
    },
  });
}
</script>
<template>
  <nav v-if="vendor" class="breadcrumbs" :aria-label="t('Breadcrumb')">
    <RouterLink to="/all">{{ t("All applications") }}</RouterLink
    ><span>›</span
    ><span>{{ vendorName(vendorData.data.value, vendor) }}</span>
  </nav>
  <div class="page-heading">
    <div>
      <h1>
        {{
          vendor
            ? vendorName(vendorData.data.value, vendor)
            : t("All applications")
        }}
      </h1>
      <p v-if="vendor" class="muted">
        {{ vendorData.data.value?.description[language] }}
      </p>
    </div>
    <img
      v-if="vendor && vendorData.data.value?.icon"
      :src="vendorData.data.value.icon"
      alt=""
      width="48"
      height="48"
    />
  </div>
  <label class="catalog-search"
    ><span class="sr-only">{{ t("Search by ID or name") }}</span
    ><input
      v-model="search"
      type="search"
      maxlength="128"
      :placeholder="t('Search by ID or name')"
  /></label>
  <p
    v-if="error || (vendor && vendorData.error.value)"
    class="error"
    role="alert"
  >
    {{ errorText(error || vendorData.error.value) }}
    <IconButton @click="refresh" icon="refresh" :label="t('Retry')" />
  </p>
  <p v-if="loading" role="status">{{ t("Loading…") }}</p>
  <template v-if="data"
    ><PublicCards :apps="data.items" />
    <p v-if="!data.items.length" class="empty">
      {{ t("No applications are available.") }}
    </p>
    <PageNavigation
      :label="t('Application pages')"
      :page="data.page"
      :total="data.total"
      :total-pages="data.total_pages"
      :previous="data.page > 1"
      :next="data.page < data.total_pages"
      :loading="loading"
      @previous="go(data.page - 1)"
      @next="go(data.page + 1)"
      @go="go"
      @refresh="refresh"
  /></template>
</template>
