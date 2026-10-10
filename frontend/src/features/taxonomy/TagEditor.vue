<script setup lang="ts">
import { ref, useId } from "vue";
import { Plus } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { codePointLength } from "@/shared/forms";
import { Button, Input, Tag } from "@/shared/ui";
import { foldText } from "./queries";

/**
 * Private search tags. Type a tag and press Enter (or the add button); a
 * leading `#` is dropped and duplicates are ignored, as the server does.
 * `commit()` adds the text still being typed (call it before saving).
 */
const tags = defineModel<string[]>({ required: true });
defineProps<{ label: string; disabled?: boolean }>();
const { t } = useI18n();
const id = useId();
const text = ref("");
const message = ref("");
const MAX_TAGS = 50;
const MAX_LENGTH = 64;

/** Adds the typed tag; returns false when it is invalid (message shown). */
function commit(): boolean {
  const value = text.value.normalize("NFC").trim().replace(/^#+/, "").trim();
  message.value = "";
  if (!value) {
    text.value = "";
    return true;
  }
  if (codePointLength(value) > MAX_LENGTH) {
    message.value = t("taxonomy.tags.tooLong", { max: MAX_LENGTH });
    return false;
  }
  if (tags.value.length >= MAX_TAGS) {
    message.value = t("taxonomy.tags.tooMany", { max: MAX_TAGS });
    return false;
  }
  if (!tags.value.some((tag) => foldText(tag) === foldText(value))) {
    tags.value = [...tags.value, value];
  }
  text.value = "";
  return true;
}

function onEnter(event: KeyboardEvent): void {
  // Enter that confirms an IME composition keeps editing.
  if (event.isComposing) return;
  event.preventDefault();
  commit();
}

defineExpose({ commit, hasText: () => text.value.trim() !== "" });
</script>

<template>
  <div class="flex flex-col gap-2">
    <ul v-if="tags.length" class="flex flex-wrap gap-1.5" :aria-label="label">
      <li v-for="tag in tags" :key="tag">
        <Tag
          :label="`#${tag}`"
          removable
          :disabled="disabled"
          @remove="tags = tags.filter((value) => value !== tag)"
        />
      </li>
    </ul>
    <div class="flex max-w-md gap-2">
      <Input
        :id="id"
        v-model="text"
        :aria-label="t('taxonomy.tags.new')"
        :aria-describedby="`${id}-hint`"
        :placeholder="t('taxonomy.tags.placeholder')"
        :disabled="disabled"
        maxlength="128"
        spellcheck="false"
        autocomplete="off"
        @keydown.enter="onEnter"
      />
      <Button :disabled="disabled || !text.trim()" @click="commit">
        <Plus aria-hidden="true" /> {{ t("taxonomy.tags.add") }}
      </Button>
    </div>
    <p :id="`${id}-hint`" class="text-xs text-muted">{{ t("taxonomy.tags.hint") }}</p>
    <p v-if="message" class="text-xs text-danger" role="alert">{{ message }}</p>
  </div>
</template>
