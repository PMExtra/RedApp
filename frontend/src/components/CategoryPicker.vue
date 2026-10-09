<script setup lang="ts">
import Icon from "./Icon.vue";
import { computed, ref, useId } from "vue";
import { foldText, type CategoryOption } from "../taxonomy";
import { language, t } from "../i18n";
// Searchable multi-select. Typed names that match no category stay in the draft until the App is saved.
const props = defineProps<{
  modelValue: string[];
  pending: string[];
  categories: CategoryOption[];
  label: string;
  disabled?: boolean;
}>();
const emit = defineEmits<{
  "update:modelValue": [string[]];
  "update:pending": [string[]];
}>();
const id = useId();
const query = ref(""),
  open = ref(false),
  active = ref(-1),
  message = ref("");
const root = ref<HTMLElement>();
const labelOf = (item: CategoryOption) => item.name[language.value] || item.id;
const selected = computed(() => new Set(props.modelValue));
const matches = computed(() => {
  const text = foldText(query.value.trim());
  return props.categories.filter(
    (item) =>
      !text ||
      [item.id, item.name.en, item.name["zh-CN"]].some((value) =>
        foldText(value).includes(text),
      ),
  );
});
const chips = computed(() => [
  ...props.modelValue.map((value) => ({
    key: `id:${value}`,
    label: labelOf(props.categories.find((item) => item.id === value) || { id: value, name: { en: value, "zh-CN": value } }),
    pending: false,
    value,
  })),
  ...props.pending.map((value) => ({ key: `new:${value}`, label: value, pending: true, value })),
]);
function toggle(item: CategoryOption) {
  message.value = "";
  emit(
    "update:modelValue",
    selected.value.has(item.id)
      ? props.modelValue.filter((value) => value !== item.id)
      : [...props.modelValue, item.id],
  );
}
function remove(chip: { pending: boolean; value: string }) {
  if (chip.pending) emit("update:pending", props.pending.filter((value) => value !== chip.value));
  else emit("update:modelValue", props.modelValue.filter((value) => value !== chip.value));
}
// Exact names in either language reuse the existing category; several matches need an explicit choice.
function addTyped() {
  const name = query.value.trim().normalize("NFC");
  if (!name) return;
  const key = foldText(name);
  const same = props.categories.filter((item) => foldText(item.name.en) === key || foldText(item.name["zh-CN"]) === key);
  message.value = "";
  if (same.length > 1) {
    message.value = t("This name matches several categories. Choose one from the list.");
    open.value = true;
    return;
  }
  if (same.length === 1) {
    if (!selected.value.has(same[0]!.id)) emit("update:modelValue", [...props.modelValue, same[0]!.id]);
  } else if (!props.pending.some((value) => foldText(value) === key)) {
    if ([...name].length > 64) {
      message.value = t("Category names can have at most 64 characters.");
      return;
    }
    emit("update:pending", [...props.pending, name]);
  }
  query.value = "";
  active.value = -1;
}
function enter(event: KeyboardEvent) {
  // Confirming an IME composition must not add a category.
  if (event.isComposing || event.keyCode === 229) return;
  event.preventDefault();
  if (open.value && active.value >= 0 && matches.value[active.value]) toggle(matches.value[active.value]!);
  else addTyped();
}
function move(step: number) {
  open.value = true;
  const count = matches.value.length;
  if (count) active.value = (active.value + step + count) % count;
}
function leave(event: FocusEvent) {
  if (!root.value?.contains(event.relatedTarget as Node | null)) {
    open.value = false;
    active.value = -1;
  }
}
</script>
<template>
  <div ref="root" class="category-picker" :class="{ disabled }" @focusout="leave">
    <ul class="category-chips" :aria-label="label">
      <li v-for="chip in chips" :key="chip.key" :class="{ pending: chip.pending }">
        <span>{{ chip.label }}</span
        ><small v-if="chip.pending" class="muted">{{ t("New") }}</small
        ><button
          type="button"
          class="chip-remove"
          :disabled="disabled"
          :aria-label="`${t('Remove')}: ${chip.label}`"
          @click="remove(chip)"
        >
          <Icon name="close" :size="14" />
        </button>
      </li>
      <li class="category-input">
        <input
          v-model="query"
          role="combobox"
          autocomplete="off"
          spellcheck="false"
          maxlength="128"
          :aria-label="label"
          :placeholder="t('Search or type a new category, then press Enter')"
          :aria-expanded="open"
          :aria-controls="`${id}-list`"
          :aria-activedescendant="open && active >= 0 ? `${id}-${active}` : undefined"
          :disabled="disabled"
          @focus="open = true"
          @input="open = true; active = -1; message = ''"
          @keydown.down.prevent="move(1)"
          @keydown.up.prevent="move(-1)"
          @keydown.enter="enter"
          @keydown.esc="open = false"
        />
      </li>
    </ul>
    <ul
      v-show="open && !disabled"
      :id="`${id}-list`"
      class="category-options"
      role="listbox"
      aria-multiselectable="true"
      :aria-label="label"
    >
      <li
        v-for="(item, index) in matches"
        :id="`${id}-${index}`"
        :key="item.id"
        role="option"
        tabindex="-1"
        :aria-selected="selected.has(item.id)"
        :class="{ active: index === active }"
        @mousedown.prevent
        @click="toggle(item)"
      >
        <Icon v-if="selected.has(item.id)" name="check" :size="16" /><span>{{ labelOf(item) }}</span>
      </li>
      <li v-if="!matches.length" class="muted" role="presentation">
        {{ query.trim() ? t("Press Enter to add this new category.") : t("No categories yet.") }}
      </li>
    </ul>
    <p v-if="message" class="error small-text" role="alert">{{ message }}</p>
  </div>
</template>
