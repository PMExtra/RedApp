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
 */
const props = defineProps<{
  provider: ProviderKey;
  disabled?: boolean;
  /** FieldReset props per path (overlay editing); omit when creating. */
  resetBinding?: (path: string) => ResetBinding;
}>();
const emit = defineEmits<{ reset: [path: string] }>();
const { t } = useI18n();

const httpCache = computed(() => props.provider === "http-cache");
const release = computed(() => isReleaseProvider(props.provider));
const strategies = computed(() => [
  { value: "ordered", label: t("directory.source.strategies.ordered") },
  { value: "round_robin", label: t("directory.source.strategies.round_robin") },
  { value: "random", label: t("directory.source.strategies.random") },
]);
</script>

<template>
  <div v-if="httpCache" class="flex flex-col gap-5">
    <SourceUrlList :disabled="disabled">
      <template v-if="resetBinding" #reset>
        <FieldReset
          v-bind="resetBinding('base_urls')"
          :disabled="disabled"
          @reset="emit('reset', 'base_urls')"
        />
      </template>
    </SourceUrlList>

    <div class="flex items-start gap-2">
      <div class="min-w-0 flex-1">
        <FormField
          v-slot="{ field }"
          name="source_strategy"
          :label="t('directory.source.strategy')"
          :description="t('directory.source.strategyHint')"
        >
          <RadioGroup
            v-bind="field"
            :options="strategies"
            orientation="horizontal"
            :disabled="disabled"
          />
        </FormField>
      </div>
      <FieldReset
        v-if="resetBinding"
        v-bind="resetBinding('source_strategy')"
        :disabled="disabled"
        @reset="emit('reset', 'source_strategy')"
      />
    </div>

    <div class="flex items-start gap-2">
      <div class="min-w-0 max-w-xs flex-1">
        <FormField
          v-slot="{ field }"
          name="cache_ttl_seconds"
          :label="t('directory.source.ttl')"
          :description="t('directory.source.ttlHint')"
          required
        >
          <NumberInput
            v-bind="field"
            :min="0"
            :max="86400"
            :step="1"
            unit="s"
            :disabled="disabled"
          />
        </FormField>
      </div>
      <FieldReset
        v-if="resetBinding"
        v-bind="resetBinding('cache_ttl_seconds')"
        :disabled="disabled"
        @reset="emit('reset', 'cache_ttl_seconds')"
      />
    </div>
  </div>

  <div v-else-if="release" class="flex items-start gap-2">
    <div class="min-w-0 flex-1">
      <FormField
        v-slot="{ field }"
        name="base_url"
        :label="t('directory.source.baseUrl')"
        :description="t('directory.source.baseUrlHint')"
        required
      >
        <Input
          v-bind="field"
          type="url"
          inputmode="url"
          spellcheck="false"
          autocomplete="off"
          maxlength="4096"
          :disabled="disabled"
        />
      </FormField>
    </div>
    <FieldReset
      v-if="resetBinding"
      v-bind="resetBinding('base_url')"
      :disabled="disabled"
      @reset="emit('reset', 'base_url')"
    />
  </div>
</template>
