<script setup lang="ts">
import { computed } from "vue";
import { RefreshCw } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { describeError } from "@/shared/api";
import { useFormat } from "@/shared/i18n";
import { Field, IconButton, Select, type SelectOption } from "@/shared/ui";
import { useSources, type SourceEpoch } from "./queries";

/**
 * Picks the source epoch to inspect or clean; `null` is the current epoch.
 * Old epochs stay listed while they own cached data.
 */
const model = defineModel<number | null>({ default: null });
const props = defineProps<{ vendor: string; app: string; disabled?: boolean }>();
const { t } = useI18n();
const format = useFormat();
const sources = useSources(
  () => props.vendor,
  () => props.app,
);
const items = computed(() => sources.data.value?.items ?? []);

function upstream(source: SourceEpoch): string {
  return source.base_urls?.length ? source.base_urls.join(" → ") : (source.base_url ?? "");
}

function label(source: SourceEpoch): string {
  const name = t(source.current ? "releases.sources.current" : "releases.sources.historical", {
    epoch: source.epoch,
  });
  const url = upstream(source);
  return url ? `${name} · ${url}` : name;
}

const options = computed<SelectOption[]>(() => {
  const current = items.value.find((source) => source.current);
  return [
    {
      value: "current",
      label: current ? label(current) : t("releases.sources.currentUnknown"),
    },
    ...items.value
      .filter((source) => !source.current)
      .reverse()
      .map((source) => ({ value: String(source.epoch), label: label(source) })),
  ];
});
const value = computed({
  get: () => (model.value === null ? "current" : String(model.value)),
  set: (next: string | undefined) => {
    model.value = !next || next === "current" ? null : Number(next);
  },
});
const selected = computed(() =>
  model.value === null
    ? items.value.find((source) => source.current)
    : items.value.find((source) => source.epoch === model.value),
);
const description = computed(() => {
  const source = selected.value;
  if (!source) return t("releases.sources.hint");
  const parts = [t("releases.sources.created", { time: format.dateTime(source.created_at) })];
  if (!source.active) parts.push(t("releases.sources.inactive"));
  if (source.source_strategy) parts.push(t(`releases.sources.strategy.${source.source_strategy}`));
  return [...parts, t("releases.sources.hint")].join(" · ");
});
const error = computed(() =>
  sources.error.value ? describeError(sources.error.value).message : undefined,
);
</script>

<template>
  <div class="flex items-start gap-2">
    <Field
      v-slot="{ control }"
      class="min-w-0 flex-1"
      :label="t('releases.sources.label')"
      :description="description"
      :error="error"
    >
      <Select
        v-bind="control"
        v-model="value"
        :options="options"
        :disabled="disabled || sources.isPending.value"
      />
    </Field>
    <IconButton
      :label="t('releases.sources.reload')"
      variant="secondary"
      :loading="sources.isFetching.value"
      class="mt-6.5"
      @click="sources.refetch()"
    >
      <RefreshCw aria-hidden="true" />
    </IconButton>
  </div>
</template>
