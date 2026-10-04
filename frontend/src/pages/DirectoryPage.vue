<script setup lang="ts">
import { computed, onUnmounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useNumberedCollection } from "../composables/useNumberedCollection";
import VendorCard, { type Card } from "../components/VendorCard.vue";
import PageNavigation from "../components/PageNavigation.vue";
import SelectMenu from "../components/SelectMenu.vue";
import { errorText, t } from "../i18n";
const route = useRoute(),
  router = useRouter();
const query = ref(typeof route.query.q === "string" ? route.query.q : ""),
  search = ref(query.value),
  state = ref(
    ["current", "disabled", "deleted"].includes(String(route.query.state))
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
onUnmounted(() => clearTimeout(debounce));
</script>
<template>
  <section class="directory-page">
    <div class="page-heading">
      <div>
        <h1>{{ t("Vendors and applications") }}</h1>
        <p class="muted">
          {{ t("Manage application details, providers and availability.") }}
        </p>
      </div>
      <RouterLink to="/admin/vendors/new" class="button-link">{{
        t("Add vendor")
      }}</RouterLink>
    </div>
    <div class="directory-toolbar">
      <label class="directory-search"
        >{{ t("Search vendors and applications")
        }}<input
          v-model="search"
          type="search"
          maxlength="128"
          :placeholder="t('Search by ID or name')"
      /></label>
      <div class="field-label">
        <span>{{ t("Show") }}</span
        ><SelectMenu
          v-model="state"
          :label="t('Show')"
          :options="[
            { value: 'current', label: t('Current') },
            { value: 'disabled', label: t('Disabled') },
            { value: 'deleted', label: t('Deleted') },
          ]"
        />
      </div>
    </div>
    <p v-if="list.error" class="error" role="alert">
      {{ errorText(list.error)
      }}<button class="secondary" @click="list.refresh">
        {{ t("Retry") }}
      </button>
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
                "Add a vendor, then create an application and choose its provider. No applications are created automatically.",
              )
        }}
      </p>
      <RouterLink
        v-if="!query && state === 'current'"
        to="/admin/vendors/new"
        class="button-link"
        >{{ t("Add vendor") }}</RouterLink
      >
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
