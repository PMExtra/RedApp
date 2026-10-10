<script setup lang="ts">
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useLocalized } from "@/shared/i18n";
import { useDebounced } from "@/shared/lib";
import { Combobox, EntityIcon, Field, type ComboboxOption } from "@/shared/ui";
import { useAppSearch, type AppListItem } from "./queries";

/** Searches applications of all vendors (`listApps`) and emits the chosen one. */
const props = defineProps<{
  label: string;
  description?: string;
  /** Keys that cannot be chosen again (shown disabled). */
  exclude?: readonly string[];
  disabled?: boolean;
}>();
const emit = defineEmits<{ select: [app: AppListItem] }>();
const { t, locale } = useI18n();
const localized = useLocalized();

const search = ref("");
const selected = ref<string>();
const debounced = useDebounced(search, 200);
const apps = useAppSearch(debounced, locale);
const excluded = computed(() => new Set(props.exclude ?? []));

interface AppOption extends ComboboxOption {
  app: AppListItem;
}
const options = computed<AppOption[]>(() =>
  (apps.data.value?.items ?? []).map((app) => ({
    value: app.key,
    label: `${localized(app.name)} (${app.key})`,
    disabled: excluded.value.has(app.key),
    app,
  })),
);

function choose(option: AppOption) {
  if (excluded.value.has(option.value)) return;
  emit("select", option.app);
  search.value = "";
  selected.value = undefined;
}
</script>

<template>
  <Field v-slot="{ control }" :label="label" :description="description">
    <Combobox
      v-bind="control"
      v-model="selected"
      v-model:search="search"
      :options="options"
      :loading="apps.isFetching.value"
      :disabled="disabled"
      :placeholder="t('settings.homepage.searchPlaceholder')"
      @select="choose"
    >
      <template #option="{ option }">
        <span class="flex min-w-0 items-center gap-2">
          <EntityIcon :src="option.app.icon" size="xs" />
          <span class="truncate">{{ localized(option.app.name) }}</span>
          <code class="ms-auto shrink-0 text-xs text-muted">{{ option.value }}</code>
          <span v-if="option.disabled" class="shrink-0 text-xs text-muted">
            {{ t("settings.homepage.alreadyPinned") }}
          </span>
        </span>
      </template>
    </Combobox>
  </Field>
</template>
