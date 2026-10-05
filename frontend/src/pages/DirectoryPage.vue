<script setup lang="ts">
import Icon from "../components/Icon.vue";
import IconButton from "../components/IconButton.vue";
import { computed, onUnmounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useNumberedCollection } from "../composables/useNumberedCollection";
import VendorCard, { type Card } from "../components/VendorCard.vue";
import PageNavigation from "../components/PageNavigation.vue";
import { errorText, t } from "../i18n";
const route = useRoute(),
  router = useRouter();
const query = ref(typeof route.query.q === "string" ? route.query.q : ""),
  search = ref(query.value),
  state = ref(
    ["current", "enabled", "disabled"].includes(String(route.query.state))
      ? String(route.query.state)
      : "current",
  );
const path = computed(
  () =>
    `vendors?${new URLSearchParams({ q: query.value, state: state.value })}`,
);
const list = reactive(useNumberedCollection<Card>(path, 12));
let debounce: ReturnType<typeof setTimeout> | undefined;
watch(search, (value) => {
  clearTimeout(debounce);
  debounce = setTimeout(() => (query.value = value.trim()), 250);
});
watch(
  [query, state],
  () =>
    void router.replace({
      query: {
        ...(query.value ? { q: query.value } : {}),
        ...(state.value === "current" ? {} : { state: state.value }),
      },
    }),
);
watch(
  () => route.query,
  (value) => {
    const next = typeof value.q === "string" ? value.q : "";
    if (next !== query.value) {
      clearTimeout(debounce);
      query.value = next;
      search.value = next;
    }
    state.value = ["current", "enabled", "disabled"].includes(
      String(value.state),
    )
      ? String(value.state)
      : "current";
    if (
      value.state &&
      !["current", "enabled", "disabled"].includes(String(value.state))
    )
      void router.replace({ query: { ...value, state: undefined } });
  },
  { immediate: true },
);
onUnmounted(() => clearTimeout(debounce));
</script>
<template>
  <section class="directory-page">
    <p v-if="route.query.cleanup === 'pending'" class="notice" role="status">
      {{
        t(
          "Deleted. Some stored files are awaiting cleanup; restart the server to retry cleanup.",
        )
      }}
    </p>
    <div class="page-heading">
      <div>
        <h1>{{ t("Vendors and applications") }}</h1>
        <p class="muted">
          {{ t("Manage application details, providers and availability.") }}
        </p>
      </div>
      <RouterLink
        to="/admin/vendors/new"
        class="button-link icon-button"
        :aria-label="t('Add vendor')"
        :title="t('Add vendor')"
        ><Icon name="plus"
      /></RouterLink>
    </div>
    <div class="directory-toolbar">
      <label class="directory-search"
        ><span class="sr-only">{{ t("Search vendors and applications") }}</span
        ><input
          v-model="search"
          type="search"
          maxlength="128"
          :placeholder="t('Search by ID or name')"
      /></label>
      <div
        class="status-filter button-group"
        role="group"
        :aria-label="t('Availability')"
      >
        <button
          v-for="option in [
            { value: 'current', label: t('All') },
            { value: 'enabled', label: t('Enabled') },
            { value: 'disabled', label: t('Disabled') },
          ]"
          :key="option.value"
          type="button"
          class="secondary"
          :aria-pressed="state === option.value"
          @click="state = option.value"
        >
          {{ option.label }}
        </button>
      </div>
    </div>
    <p v-if="list.error" class="error" role="alert">
      {{ errorText(list.error)
      }}<IconButton
        class="secondary"
        @click="list.refresh"
        icon="refresh"
        :label="t('Retry')"
      />
    </p>
    <p v-if="list.loading && !list.loaded" role="status">{{ t("Loading…") }}</p>
    <div class="vendor-grid">
      <VendorCard
        v-for="vendor in list.items"
        :key="vendor.uid"
        :vendor="vendor"
        :query="query"
        :state="state"
      />
    </div>
    <section v-if="list.loaded && !list.items.length" class="panel empty-state">
      <h2>
        {{
          query || state !== "current"
            ? t("No vendors in this view.")
            : t("Create your first vendor")
        }}
      </h2>
      <p>
        {{
          query || state !== "current"
            ? t("Try another search or view.")
            : t(
                "Add a vendor, then create an application and choose its provider. Built-in applications start disabled.",
              )
        }}
      </p>
      <RouterLink
        v-if="!query && state === 'current'"
        to="/admin/vendors/new"
        class="button-link icon-button"
        :aria-label="t('Add vendor')"
        :title="t('Add vendor')"
        ><Icon name="plus"
      /></RouterLink>
    </section>
    <PageNavigation
      :label="t('Vendor pages')"
      :page="list.page"
      :total="list.total"
      :total-pages="list.totalPages"
      :previous="list.previousAvailable"
      :next="list.nextAvailable"
      :loading="list.loading"
      @previous="list.previous"
      @next="list.next"
      @go="list.go"
      @refresh="list.refresh"
    />
  </section>
</template>
