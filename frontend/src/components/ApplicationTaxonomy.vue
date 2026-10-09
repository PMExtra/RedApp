<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from "vue";
import { allCategories, type CategoryOption } from "../taxonomy";
import { isCancellation } from "../api";
import { useConfiguration } from "../composables/useConfiguration";
import { errorText, t } from "../i18n";
import CategoryPicker from "./CategoryPicker.vue";
import TagEditor from "./TagEditor.vue";
import FieldReset from "./FieldReset.vue";
import IconButton from "./IconButton.vue";
const props = defineProps<{ application: string; readonly?: boolean }>();
// New category names stay in this draft until the App is saved; the server creates and binds them atomically.
const pending = ref<string[]>([]);
const tagEditor = ref<InstanceType<typeof TagEditor>>();
const {
  draft,
  configuration,
  unsets,
  loading,
  saving,
  error,
  saved,
  saveWith,
  mark,
  restore,
  load,
  modified,
} = useConfiguration<{ categories: string[]; tags: string[] }>(
  computed(() => `apps/${props.application}/configuration`),
  "",
  ["categories", "tags"],
  () => void refreshCategories(),
);
// A new baseline (load, save or another App) replaces the draft, including typed additions.
watch(configuration, () => {
  pending.value = [];
  tagEditor.value?.cancel();
});
const categories = ref<CategoryOption[]>([]),
  listError = ref<unknown>();
let controller: AbortController | undefined,
  ticket = 0;
async function refreshCategories() {
  controller?.abort();
  const attempt = ++ticket;
  controller = new AbortController();
  listError.value = undefined;
  try {
    const value = await allCategories(controller.signal);
    if (attempt === ticket) categories.value = value;
  } catch (reason) {
    if (attempt === ticket && !isCancellation(reason)) listError.value = reason;
  }
}
watch(
  () => props.application,
  () => {
    categories.value = [];
    void refreshCategories();
  },
  { immediate: true },
);
onUnmounted(() => {
  ticket++;
  controller?.abort();
});
const busy = computed(() => !!props.readonly || loading.value || saving.value);
function chooseCategories(value: string[]) {
  if (!draft.value) return;
  draft.value.categories = value;
  mark("categories");
}
function choosePending(value: string[]) {
  pending.value = value;
  mark("categories");
}
function chooseTags(value: string[]) {
  if (!draft.value) return;
  draft.value.tags = value;
  mark("tags");
}
function reset(field: "categories" | "tags") {
  if (field === "categories") pending.value = [];
  else tagEditor.value?.cancel();
  restore(field);
}
function reload() {
  void load();
  void refreshCategories();
}
async function submit() {
  // A tag still being typed belongs to this save.
  if (tagEditor.value && !tagEditor.value.commit()) return;
  await saveWith(pending.value.length ? { new_categories: [...pending.value] } : {});
}
</script>
<template>
  <section class="panel application-taxonomy">
    <h2>{{ t("Categories and tags") }}</h2>
    <p v-if="error || listError" class="error" role="alert">
      {{ errorText(error || listError) }}
    </p>
    <p v-if="saved" class="notice" role="status">{{ t("Changes saved.") }}</p>
    <p v-if="loading" role="status">{{ t("Loading…") }}</p>
    <p v-if="configuration?.template_missing" class="notice">
      {{ t("Template unavailable; the last accepted defaults remain in use.") }}
    </p>
    <form v-if="draft" @submit.prevent="submit">
      <div class="field-label">
        <div class="field-heading">
          <span>{{ t("Categories") }}</span>
          <FieldReset
            :configuration="configuration"
            path="categories"
            :label="t('Categories')"
            :modified="modified('categories') || pending.length > 0"
            :restored="unsets.has('categories')"
            :disabled="busy"
            @reset="reset('categories')"
          />
        </div>
        <CategoryPicker
          :model-value="draft.categories"
          :pending="pending"
          :categories="categories"
          :label="t('Categories')"
          :disabled="busy"
          @update:model-value="chooseCategories"
          @update:pending="choosePending"
        />
      </div>
      <div class="field-label">
        <div class="field-heading">
          <span>{{ t("Tags") }}</span>
          <FieldReset
            :configuration="configuration"
            path="tags"
            :label="t('Tags')"
            :modified="modified('tags')"
            :restored="unsets.has('tags')"
            :disabled="busy"
            @reset="reset('tags')"
          />
        </div>
        <TagEditor
          ref="tagEditor"
          :model-value="draft.tags"
          :label="t('Tags')"
          :disabled="busy"
          @update:model-value="chooseTags"
        />
        <p class="muted small-text">
          {{ t("Tags are private search keywords and are not shown on public pages.") }}
        </p>
      </div>
      <div class="form-actions">
        <button :disabled="busy">
          {{ saving ? t("Saving…") : t("Save") }}</button
        ><IconButton
          type="button"
          class="secondary"
          :disabled="loading || saving"
          icon="refresh"
          :label="t('Reload')"
          @click="reload"
        />
      </div>
    </form>
  </section>
</template>
