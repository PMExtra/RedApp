<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { keepPreviousData, useQuery } from "@tanstack/vue-query";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { api, queryKey, unwrap } from "@/shared/api";
import { useLocalized } from "@/shared/i18n";
import { useDebounced } from "@/shared/lib";
import { Combobox, EntityIcon, type ComboboxOption } from "@/shared/ui";

interface SearchOption extends ComboboxOption {
  kind: "vendor" | "app";
  icon: string;
}

/**
 * Header search with suggestions: `searchCatalog` for typed text, popular
 * applications (`getHome` ranking) while empty. Choosing a suggestion opens
 * `/{key}`; Enter without a highlighted suggestion opens `/all?q=`.
 */
const { t } = useI18n();
const localized = useLocalized();
const route = useRoute();
const router = useRouter();
const search = ref(typeof route.query.q === "string" ? route.query.q : "");
const selected = ref<string>();
const term = useDebounced(
  computed(() => search.value.trim()),
  200,
);

// The box mirrors `?q=` and is cleared when navigating elsewhere.
watch(
  () => route.fullPath,
  () => {
    search.value = typeof route.query.q === "string" ? route.query.q : "";
  },
);

const results = useQuery({
  queryKey: computed(() => queryKey("searchCatalog", { q: term.value })),
  queryFn: ({ signal }) =>
    unwrap(api.GET("/api/search", { params: { query: { q: term.value } }, signal })),
  enabled: computed(() => term.value !== ""),
  placeholderData: keepPreviousData,
});
const home = useQuery({
  queryKey: queryKey("getHome"),
  queryFn: ({ signal }) => unwrap(api.GET("/api/home", { signal })),
  enabled: computed(() => term.value === ""),
});

const options = computed<SearchOption[]>(() => {
  if (term.value === "") {
    return (home.data.value?.ranking ?? []).slice(0, 6).map(({ app }) => ({
      value: app.key,
      label: localized(app.name),
      kind: "app" as const,
      icon: app.icon,
    }));
  }
  return (results.data.value?.items ?? []).map((hit) => ({
    value: hit.key,
    label: localized(hit.name),
    kind: hit.kind,
    icon: (hit.localized_icons && localized(hit.localized_icons)) || hit.icon,
  }));
});
const loading = computed(() =>
  term.value === "" ? home.isFetching.value : results.isFetching.value,
);

async function open(option: SearchOption) {
  selected.value = undefined;
  await router.push(`/${option.value}`);
}

async function submit(text: string) {
  const q = text.trim();
  await router.push(q ? { path: "/all", query: { q } } : { path: "/all" });
}
</script>

<template>
  <Combobox
    v-model="selected"
    v-model:search="search"
    :options="options"
    :loading="loading"
    :placeholder="t('search.placeholder')"
    :aria-label="t('search.label')"
    keep-search
    maxlength="128"
    type="search"
    class="w-full max-w-md"
    @select="open"
    @submit="submit"
  >
    <template #option="{ option }">
      <EntityIcon :src="option.icon" size="sm" />
      <span class="flex min-w-0 flex-col">
        <span class="truncate">{{ option.label }}</span>
        <span class="truncate text-xs text-muted">
          {{ option.kind === "vendor" ? t("search.vendor") : t("search.app") }} ·
          {{ option.value }}
        </span>
      </span>
    </template>
  </Combobox>
</template>
