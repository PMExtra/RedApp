<script setup lang="ts">
import { computed, ref, useId } from "vue";
import { useI18n } from "vue-i18n";
import { codePointLength } from "@/shared/forms";
import { useLocalized } from "@/shared/i18n";
import { Combobox, Tag, type ComboboxOption } from "@/shared/ui";
import { foldText, type Category } from "./queries";

/**
 * Multi-select of categories. Typing a name that matches no category and
 * pressing Enter adds it as pending; pending names are created when the
 * application is saved (`new_categories`).
 */
const selected = defineModel<string[]>({ required: true });
const pending = defineModel<string[]>("pending", { required: true });
const props = defineProps<{ categories: Category[]; label: string; disabled?: boolean }>();
const { t } = useI18n();
const localized = useLocalized();
const id = useId();
const search = ref("");
const chosen = ref<string>();
const message = ref("");
const MAX_NAME = 64;
const MAX_CATEGORIES = 32;

const byId = computed(() => new Map(props.categories.map((item) => [item.id, item])));
const options = computed<ComboboxOption[]>(() => {
  const text = foldText(search.value);
  return props.categories
    .filter((item) => !selected.value.includes(item.id))
    .filter(
      (item) =>
        !text ||
        [item.id, item.name.en, item.name["zh-CN"]].some((value) => foldText(value).includes(text)),
    )
    .map((item) => ({ value: item.id, label: localized(item.name) || item.id }));
});
const count = computed(() => selected.value.length + pending.value.length);

function select(option: ComboboxOption): void {
  message.value = "";
  chosen.value = undefined;
  if (count.value >= MAX_CATEGORIES) {
    message.value = t("taxonomy.categories.tooMany", { max: MAX_CATEGORIES });
    return;
  }
  if (!selected.value.includes(option.value)) selected.value = [...selected.value, option.value];
  search.value = "";
}

/** Enter on typed text: reuse a category with that exact name or add a pending one. */
function addTyped(text: string): void {
  const name = text.normalize("NFC").trim();
  if (!name) return;
  message.value = "";
  const key = foldText(name);
  const same = props.categories.filter(
    (item) => foldText(item.name.en) === key || foldText(item.name["zh-CN"]) === key,
  );
  if (same.length > 1) {
    message.value = t("taxonomy.categories.ambiguous");
    return;
  }
  if (count.value >= MAX_CATEGORIES) {
    message.value = t("taxonomy.categories.tooMany", { max: MAX_CATEGORIES });
    return;
  }
  const match = same[0];
  if (match) {
    if (!selected.value.includes(match.id)) selected.value = [...selected.value, match.id];
  } else if (!pending.value.some((value) => foldText(value) === key)) {
    if (codePointLength(name) > MAX_NAME) {
      message.value = t("taxonomy.categories.tooLong", { max: MAX_NAME });
      return;
    }
    pending.value = [...pending.value, name];
  }
  search.value = "";
}

/**
 * Enter in the picker never submits the surrounding form. With no matching
 * suggestion it adds the typed name (the list may still report a stale
 * highlight, so the combobox itself would not).
 */
function onEnter(event: KeyboardEvent): void {
  event.preventDefault();
  if (!event.isComposing && options.value.length === 0) addTyped(search.value);
}

function categoryLabel(categoryId: string): string {
  const item = byId.value.get(categoryId);
  return item ? localized(item.name) || item.id : categoryId;
}
</script>

<template>
  <div class="flex flex-col gap-2">
    <ul
      v-if="selected.length || pending.length"
      class="flex flex-wrap gap-1.5"
      :aria-label="t('taxonomy.categories.selected')"
    >
      <li v-for="categoryId in selected" :key="`id:${categoryId}`">
        <Tag
          :label="categoryLabel(categoryId)"
          removable
          :disabled="disabled"
          @remove="selected = selected.filter((value) => value !== categoryId)"
        />
      </li>
      <li v-for="name in pending" :key="`new:${name}`">
        <Tag
          :label="t('taxonomy.categories.pendingLabel', { name })"
          removable
          :disabled="disabled"
          @remove="pending = pending.filter((value) => value !== name)"
        />
      </li>
    </ul>
    <Combobox
      :id="id"
      v-model="chosen"
      v-model:search="search"
      :options="options"
      :placeholder="t('taxonomy.categories.placeholder')"
      :disabled="disabled"
      :aria-label="label"
      :aria-describedby="`${id}-hint`"
      maxlength="128"
      @select="select"
      @submit="addTyped"
      @keydown.enter="onEnter"
    />
    <p :id="`${id}-hint`" class="text-xs text-muted">{{ t("taxonomy.categories.hint") }}</p>
    <p v-if="message" class="text-xs text-danger" role="alert">{{ message }}</p>
  </div>
</template>
