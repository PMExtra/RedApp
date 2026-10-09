<script setup lang="ts">
import Icon from "./Icon.vue";
import { nextTick, ref } from "vue";
import { foldText } from "../taxonomy";
import { t } from "../i18n";
// "#" is display-only. A tag being typed becomes read-only on blur or Enter; save calls commit() first.
const props = defineProps<{ modelValue: string[]; label: string; disabled?: boolean }>();
const emit = defineEmits<{ "update:modelValue": [string[]] }>();
const maxTags = 50,
  maxLength = 64;
const editing = ref(false),
  text = ref(""),
  message = ref("");
const input = ref<HTMLInputElement>();
async function start() {
  if (props.disabled || props.modelValue.length >= maxTags) return;
  if (editing.value) commit();
  message.value = "";
  editing.value = true;
  await nextTick();
  input.value?.focus();
}
function commit() {
  if (!editing.value) return true;
  const value = text.value.normalize("NFC").trim().replace(/^#+/, "").trim();
  if ([...value].length > maxLength) {
    message.value = t("Tags can have at most 64 characters.");
    return false;
  }
  editing.value = false;
  text.value = "";
  if (!value) return true;
  if (props.modelValue.some((tag) => foldText(tag) === foldText(value))) {
    message.value = t("This tag already exists.");
    return true;
  }
  message.value = "";
  emit("update:modelValue", [...props.modelValue, value]);
  return true;
}
function cancel() {
  editing.value = false;
  text.value = "";
  message.value = "";
}
function enter(event: KeyboardEvent) {
  // Enter that confirms an IME composition keeps editing.
  if (event.isComposing || event.keyCode === 229) return;
  event.preventDefault();
  commit();
}
function remove(index: number) {
  emit("update:modelValue", props.modelValue.filter((_, i) => i !== index));
}
defineExpose({ commit, cancel });
</script>
<template>
  <div class="tag-editor" :class="{ disabled }" role="group" :aria-label="label">
    <ul class="tag-list">
      <li v-for="(tag, index) in modelValue" :key="tag" class="tag-chip">
        <span>#{{ tag }}</span
        ><button
          type="button"
          class="chip-remove"
          :disabled="disabled"
          :aria-label="`${t('Remove tag')}: #${tag}`"
          @click="remove(index)"
        >
          <Icon name="close" :size="14" />
        </button>
      </li>
      <li v-if="editing" class="tag-chip editing">
        <span aria-hidden="true">#</span
        ><input
          ref="input"
          v-model="text"
          :aria-label="t('New tag')"
          :maxlength="maxLength * 2"
          spellcheck="false"
          @blur="commit"
          @keydown.enter="enter"
          @keydown.esc.prevent="cancel"
        />
      </li>
      <li>
        <button
          type="button"
          class="tag-add"
          :disabled="disabled || modelValue.length >= maxTags"
          :aria-label="t('Add tag')"
          :title="t('Add tag')"
          @mousedown.prevent
          @click="start"
        >
          <Icon name="plus" :size="16" />
        </button>
      </li>
    </ul>
    <p v-if="message" class="error small-text" role="alert">{{ message }}</p>
  </div>
</template>
