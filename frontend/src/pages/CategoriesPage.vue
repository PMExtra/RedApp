<script setup lang="ts">
import { computed, ref } from "vue";
import { api } from "../api";
import { language, t, errorText } from "../i18n";
import { useNumberedCollection } from "../composables/useNumberedCollection";
import { useDirtyDraft } from "../composables/useDirtyDraft";
import type { CategoryItem } from "../taxonomy";
import IconButton from "../components/IconButton.vue";
import PageNavigation from "../components/PageNavigation.vue";
import FieldReset from "../components/FieldReset.vue";
// Categories are created while saving an App and pruned when unused; this page only renames them.
const search = ref("");
const collection = useNumberedCollection<CategoryItem>(
  computed(() => `categories?q=${encodeURIComponent(search.value)}`),
);
const { items, page, total, totalPages, loading, error: listingError, refresh, go } = collection;
const selected = ref<CategoryItem>(),
  name = ref({ en: "", "zh-CN": "" }),
  busy = ref(false),
  error = ref<unknown>();
const touched = ref(new Set<string>()),
  unsets = ref(new Set<string>());
const dirty = computed(() => !!selected.value && (touched.value.size > 0 || unsets.value.size > 0));
const confirmDiscard = useDirtyDraft(dirty);
const locales = ["en", "zh-CN"] as const;
function accept(item?: CategoryItem) {
  selected.value = item;
  name.value = { ...(item?.name || { en: "", "zh-CN": "" }) };
  touched.value = new Set();
  unsets.value = new Set();
}
function select(item: CategoryItem) {
  if (!confirmDiscard()) return;
  accept(item);
  error.value = undefined;
}
function mark(path: string) {
  touched.value.add(path);
  unsets.value.delete(path);
}
function restore(path: string) {
  const locale = path.slice(5) as "en" | "zh-CN";
  const defaults = selected.value?.defaults?.name as typeof name.value | undefined;
  if (!defaults) return;
  name.value[locale] = defaults[locale];
  touched.value.delete(path);
  if (selected.value?.fields[path]?.source === "custom") unsets.value.add(path);
}
async function reloadSelected() {
  if (!selected.value || !confirmDiscard()) return;
  const response = await api<{ items: CategoryItem[] }>(`categories?q=${encodeURIComponent(selected.value.id)}&limit=100`);
  const current = response.items.find((x) => x.id === selected.value?.id);
  accept(current);
  error.value = undefined;
}
async function save() {
  if (busy.value || !selected.value || !dirty.value) return;
  busy.value = true;
  error.value = undefined;
  try {
    const set: Record<string, string> = {};
    for (const path of touched.value) set[path] = name.value[path.slice(5) as "en" | "zh-CN"];
    const result = await api<CategoryItem>(
      `categories/${selected.value.id}`,
      { revision: selected.value.revision, set, unset: [...unsets.value] },
      undefined,
      {},
      "PATCH",
    );
    accept({ ...result, applications: selected.value.applications });
    await refresh();
  } catch (reason) {
    error.value = reason;
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <div class="page-heading"><h1>{{ t("Manage categories") }}</h1></div>
  <p class="muted">
    {{ t("Categories are added while editing an application and removed automatically when no application uses them. Built-in categories are kept.") }}
  </p>
  <label class="search-field"
    ><span class="sr-only">{{ t("Search by ID or name") }}</span
    ><input v-model="search" type="search" maxlength="128" :placeholder="t('Search by ID or name')"
  /></label>
  <p v-if="error || listingError" class="error" role="alert">{{ errorText(error || listingError) }}</p>
  <section class="panel">
    <div v-for="item in items" :key="item.id" class="directory-row">
      <button class="secondary" :disabled="busy" @click="select(item)">
        {{ item.name[language] }} <small>{{ item.id }}</small>
      </button>
      <small class="muted">{{ t("{count} applications", { count: item.applications }) }}</small>
    </div>
    <p v-if="!loading && !items.length" class="muted">{{ t("No categories yet.") }}</p>
    <PageNavigation
      :label="t('Categories')"
      :page="page"
      :total="total"
      :total-pages="totalPages"
      :previous="page > 1"
      :next="page < totalPages"
      :loading="loading"
      @go="go"
      @previous="go(page - 1)"
      @next="go(page + 1)"
      @refresh="refresh"
    />
  </section>
  <p v-if="selected?.template_missing" class="notice">
    {{ t("Template unavailable; the last accepted defaults remain in use.") }}
  </p>
  <form v-if="selected" class="panel" @submit.prevent="save">
    <h2>{{ selected.id }}</h2>
    <div v-for="locale in locales" :key="locale" class="resettable-field">
      <label
        >{{ locale === "en" ? "English" : "简体中文"
        }}<input
          v-model="name[locale]"
          required
          maxlength="256"
          :disabled="busy"
          @input="mark(`name.${locale}`)"
      /></label>
      <FieldReset
        :configuration="selected"
        :path="`name.${locale}`"
        :label="locale === 'en' ? 'English' : '简体中文'"
        :modified="touched.has(`name.${locale}`)"
        :restored="unsets.has(`name.${locale}`)"
        :disabled="busy"
        @reset="restore(`name.${locale}`)"
      />
    </div>
    <div class="form-actions">
      <button :disabled="busy || !dirty">{{ t("Save") }}</button
      ><IconButton icon="refresh" :label="t('Reload')" :disabled="busy" @click="reloadSelected" /><button
        type="button"
        class="secondary"
        :disabled="busy"
        @click="confirmDiscard() && accept(undefined)"
      >
        {{ t("Cancel") }}
      </button>
    </div>
  </form>
</template>
