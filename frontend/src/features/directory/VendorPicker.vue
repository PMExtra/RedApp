<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useDebounced } from "@/shared/lib";
import { useLocalized } from "@/shared/i18n";
import { Combobox, type ComboboxOption } from "@/shared/ui";
import { useVendorSearch } from "./queries";

/**
 * Vendor ID input with server-side suggestions (ID or name). Typing an ID
 * directly also works; the model is the typed or chosen vendor ID.
 */
defineOptions({ inheritAttrs: false });
const model = defineModel<string>({ required: true });
defineProps<{ disabled?: boolean; placeholder?: string }>();
const localized = useLocalized();
const search = ref(model.value);
const debounced = useDebounced(search, 200);
const vendors = useVendorSearch(debounced);
const chosen = ref<string>();

watch(search, (value) => {
  model.value = value.trim();
});
watch(model, (value) => {
  if (value !== search.value.trim()) search.value = value;
});

const options = computed<ComboboxOption[]>(() =>
  (vendors.data.value?.items ?? []).map((vendor) => ({
    value: vendor.id,
    label: `${localized(vendor.name) || vendor.id} (${vendor.id})`,
  })),
);

function select(option: ComboboxOption): void {
  model.value = option.value;
  search.value = option.value;
  chosen.value = undefined;
}
</script>

<template>
  <Combobox
    v-bind="$attrs"
    v-model="chosen"
    v-model:search="search"
    :options="options"
    :loading="vendors.isFetching.value"
    :disabled="disabled"
    :placeholder="placeholder"
    keep-search
    autocomplete="off"
    spellcheck="false"
    @select="select"
  />
</template>
