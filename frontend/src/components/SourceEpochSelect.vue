<script setup lang="ts">
import SelectMenu from "./SelectMenu.vue";
import { computed } from "vue";
import type { SourceEpoch } from "../composables/useSourceEpoch";
import { errorText, t } from "../i18n";
const props = defineProps<{
  sources: SourceEpoch[];
  loading: boolean;
  error?: unknown;
  modelValue: string;
  disabled?: boolean;
}>();
defineEmits<{ "update:modelValue": [string]; reload: [] }>();
const current = computed(() => props.sources.find((source) => source.current));
const selected = computed(() =>
  props.modelValue
    ? props.sources.find((source) => String(source.epoch) === props.modelValue)
    : current.value,
);
function title(source: SourceEpoch) {
  const urls = source.base_urls?.length
    ? source.base_urls.join(" → ")
    : source.base_url;
  const strategy = source.source_strategy
    ? ` · ${source.source_strategy === "ordered" ? t("In order") : source.source_strategy === "round_robin" ? t("Round robin") : t("Random")}`
    : "";
  return `${t("Source {epoch}", { epoch: source.epoch })} · ${urls}${strategy} · ${source.current ? t("Current source") : t("Historical source")}`;
}
</script>
<template>
  <div class="source-epoch-select">
    <label
      >{{ t("Cache source")
      }}<SelectMenu
        name="source_epoch"
        :model-value="modelValue"
        :disabled="disabled"
        :label="t('Cache source')"
        :options="[
          { value: '', label: current ? title(current) : t('Current source') },
          ...sources
            .filter((item) => !item.current)
            .map((source) => ({
              value: String(source.epoch),
              label: title(source),
            })),
        ]"
        @update:model-value="$emit('update:modelValue', $event)"
    /></label>
    <details v-if="selected?.base_urls?.length">
      <summary>{{ t("Upstream sources") }}</summary>
      <ol>
        <li v-for="url in selected.base_urls" :key="url">
          <code>{{ url }}</code>
        </li>
      </ol>
    </details>
    <button
      type="button"
      class="secondary"
      :disabled="loading || disabled"
      @click="$emit('reload')"
    >
      {{ t("Refresh sources") }}
    </button>
    <p v-if="loading" role="status">{{ t("Loading…") }}</p>
    <p v-if="error" class="error source-error" role="alert">
      {{ errorText(error) }}
    </p>
    <p class="muted small-text">
      {{
        t(
          "Historical sources retain cached files after the upstream URL changes. Cleanup affects only the selected source.",
        )
      }}
    </p>
  </div>
</template>
