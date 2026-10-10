<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Plus, X } from "@lucide/vue";
import { useField, useSubmitCount } from "vee-validate";
import { useI18n } from "vue-i18n";
import { Button, Field, IconButton, Input, SortableList } from "@/shared/ui";
import { isHttpUrl } from "./schemas";

/**
 * The ordered `base_urls` of an `http-cache` form: 1–16 URLs, reorderable by
 * drag or keyboard. Must be rendered inside a vee-validate form.
 */
defineProps<{ disabled?: boolean }>();
defineSlots<{ reset?: () => unknown }>();
const { t } = useI18n();
const MAX_SOURCES = 16;
// A literal "https://" attribute confuses vue-tsc (comment syntax); bind it.
const URL_PLACEHOLDER = "https://";

const { value: urls, setValue } = useField<string[] | undefined>("base_urls");
const submitCount = useSubmitCount();

// Rows need stable keys so focus and drag state follow the moved URL.
let nextKey = 0;
const keys = ref<number[]>([]);
watch(
  () => urls.value?.length ?? 0,
  (length) => {
    while (keys.value.length < length) keys.value.push(nextKey++);
    if (keys.value.length > length) keys.value = keys.value.slice(0, length);
  },
  { immediate: true },
);

interface Row {
  key: number;
  url: string;
}
const rows = computed<Row[]>({
  get: () => (urls.value ?? []).map((url, index) => ({ key: keys.value[index] ?? -1, url })),
  set: (next) => {
    keys.value = next.map((row) => row.key);
    setValue(next.map((row) => row.url));
  },
});

function setUrl(index: number, url: string): void {
  setValue((urls.value ?? []).map((value, i) => (i === index ? url : value)));
}

function addUrl(): void {
  setValue([...(urls.value ?? []), ""]);
}

function removeUrl(index: number): void {
  keys.value = keys.value.filter((_, i) => i !== index);
  setValue((urls.value ?? []).filter((_, i) => i !== index));
}

// Row errors are derived here: the schema reports them on `base_urls[i]`
// paths, which are not fields of their own. The add/remove buttons keep the
// list within 1–16 entries.
function rowError(index: number): string | undefined {
  if (submitCount.value === 0) return undefined;
  const list = urls.value ?? [];
  const url = (list[index] ?? "").trim();
  if (!isHttpUrl(url)) return t("directory.validation.url");
  const first = list.findIndex((value) => value.trim() === url);
  return first !== index ? t("directory.validation.urlsUnique") : undefined;
}
</script>

<template>
  <fieldset class="flex flex-col gap-2" :disabled="disabled">
    <div class="flex items-center justify-between gap-2">
      <legend class="text-sm font-medium">{{ t("directory.source.urls") }}</legend>
      <slot name="reset" />
    </div>
    <p class="text-xs text-muted">{{ t("directory.source.urlsHint") }}</p>
    <SortableList
      v-model="rows"
      :item-key="(row) => String(row.key)"
      :item-label="(row) => row.url || t('directory.source.url', { number: rows.indexOf(row) + 1 })"
      :disabled="disabled"
    >
      <template #item="{ item, index }">
        <div class="flex items-start gap-2">
          <div class="min-w-0 flex-1">
            <Field
              v-slot="{ control }"
              :label="t('directory.source.url', { number: index + 1 })"
              :error="rowError(index)"
              hide-label
            >
              <Input
                v-bind="control"
                :model-value="item.url"
                type="url"
                inputmode="url"
                spellcheck="false"
                autocomplete="off"
                maxlength="4096"
                :placeholder="URL_PLACEHOLDER"
                :disabled="disabled"
                @update:model-value="setUrl(index, $event)"
              />
            </Field>
          </div>
          <IconButton
            :label="t('directory.source.removeUrl', { number: index + 1 })"
            :disabled="disabled || rows.length <= 1"
            @click="removeUrl(index)"
          >
            <X aria-hidden="true" />
          </IconButton>
        </div>
      </template>
    </SortableList>
    <div>
      <Button size="sm" :disabled="disabled || rows.length >= MAX_SOURCES" @click="addUrl">
        <Plus aria-hidden="true" /> {{ t("directory.source.addUrl") }}
      </Button>
    </div>
  </fieldset>
</template>
