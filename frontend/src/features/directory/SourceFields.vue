<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { FieldReset } from "@/features/configuration";
import { FormField } from "@/shared/forms";
import { Input, NumberInput, RadioGroup } from "@/shared/ui";
import { isReleaseProvider } from "./links";
import type { ProviderKey } from "./queries";
import SourceUrlList from "./SourceUrlList.vue";
import type { ResetBinding } from "./types";

/**
 * Upstream fields of a vee-validate form, by provider: `http-cache` has an
 * ordered list of source URLs, a selection strategy and the default TTL;
 * release providers have one base URL; `info` and `hosted` have none.
 * `sourceLocked` (mirrored applications) keeps the URLs and the strategy
 * read-only: the upstream provides them.
 */
const props = defineProps<{
  provider: ProviderKey;
  disabled?: boolean;
  /** The source comes from an upstream RedApp; only the freshness stays editable. */
  sourceLocked?: boolean;
  /** FieldReset props per path (overlay editing); omit when creating. */
  resetBinding?: (path: string) => ResetBinding;
}>();
const emit = defineEmits<{ reset: [path: string] }>();
const { t } = useI18n();

const httpCache = computed(() => props.provider === "http-cache");
const release = computed(() => isReleaseProvider(props.provider));
const sourceDisabled = computed(() => props.disabled || props.sourceLocked);
const strategies = computed(() => [
  { value: "ordered", label: t("directory.source.strategies.ordered") },
  { value: "round_robin", label: t("directory.source.strategies.round_robin") },
  { value: "random", label: t("directory.source.strategies.random") },
]);
</script>

<template>
  <div v-if="httpCache" class="flex flex-col gap-5">
    <SourceUrlList :disabled="sourceDisabled">
      <template v-if="resetBinding && !sourceLocked" #reset>
        <FieldReset
          v-bind="resetBinding('base_urls')"
          :disabled="disabled"
          @reset="emit('reset', 'base_urls')"
        />
      </template>
    </SourceUrlList>

    <FormField
      name="source_strategy"
      :label="t('directory.source.strategy')"
      :description="t('directory.source.strategyHint')"
    >
      <template v-if="resetBinding && !sourceLocked" #label-end>
        <FieldReset
          v-bind="resetBinding('source_strategy')"
          :disabled="disabled"
          @reset="emit('reset', 'source_strategy')"
        />
      </template>
      <template #default="{ field }">
        <RadioGroup
          v-bind="field"
          :options="strategies"
          orientation="horizontal"
          :disabled="sourceDisabled"
        />
      </template>
    </FormField>

    <div class="max-w-xs">
      <FormField
        name="cache_ttl_seconds"
        :label="t('directory.source.ttl')"
        :description="t('directory.source.ttlHint')"
        required
      >
        <template v-if="resetBinding" #label-end>
          <FieldReset
            v-bind="resetBinding('cache_ttl_seconds')"
            :disabled="disabled"
            @reset="emit('reset', 'cache_ttl_seconds')"
          />
        </template>
        <template #default="{ field }">
          <NumberInput
            v-bind="field"
            :min="0"
            :max="86400"
            :step="1"
            unit="s"
            :disabled="disabled"
          />
        </template>
      </FormField>
    </div>
  </div>

  <FormField
    v-else-if="release"
    name="base_url"
    :label="t('directory.source.baseUrl')"
    :description="t('directory.source.baseUrlHint')"
    required
  >
    <template v-if="resetBinding && !sourceLocked" #label-end>
      <FieldReset
        v-bind="resetBinding('base_url')"
        :disabled="disabled"
        @reset="emit('reset', 'base_url')"
      />
    </template>
    <template #default="{ field }">
      <Input
        v-bind="field"
        type="url"
        inputmode="url"
        spellcheck="false"
        autocomplete="off"
        maxlength="4096"
        :disabled="sourceDisabled"
      />
    </template>
  </FormField>
</template>
