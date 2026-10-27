<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { z } from "zod";
import {
  FieldReset,
  OverlaySectionCard,
  TemplateMissingAlert,
  useAppConfiguration,
  useAppConfigurationPatch,
} from "@/features/configuration";
import { useLocalized } from "@/shared/i18n";
import { AsyncState } from "@/shared/ui";
import IconField from "./IconField.vue";
import { isReleaseProvider } from "./links";
import LocalizedTextFields from "./LocalizedTextFields.vue";
import ProxyCard from "./ProxyCard.vue";
import { useProviders, type App } from "./queries";
import {
  baseUrlsSchema,
  descriptionSchema,
  httpUrlSchema,
  iconSchema,
  nameSchema,
  strategySchema,
  ttlSchema,
} from "./schemas";
import SourceFields from "./SourceFields.vue";

/**
 * Application details, upstream and proxy: three cards over the application
 * configuration overlay, each saved on its own.
 */
const props = defineProps<{ app: App }>();
const { t } = useI18n();
const localized = useLocalized();
const provider = props.app.provider;
const vendor = () => props.app.vendor_id;
const appId = () => props.app.id;

const DETAILS_PATHS = ["name.en", "name.zh-CN", "description.en", "description.zh-CN", "icon"];
const detailsSchema = z.object({
  name: nameSchema,
  description: descriptionSchema,
  icon: iconSchema,
});
const SOURCE_PATHS: readonly string[] =
  provider === "http-cache"
    ? ["base_urls", "source_strategy", "cache_ttl_seconds"]
    : isReleaseProvider(provider)
      ? ["base_url"]
      : [];
const sourceSchema = z.object({
  ...(provider === "http-cache"
    ? { base_urls: baseUrlsSchema, source_strategy: strategySchema, cache_ttl_seconds: ttlSchema }
    : {}),
  ...(isReleaseProvider(provider) ? { base_url: httpUrlSchema } : {}),
});

const configuration = useAppConfiguration(vendor, appId);
const data = computed(() => configuration.data.value);
const reloading = computed(() => configuration.isFetching.value);
const readOnly = computed(() => props.app.deleted_at !== null);
// One mutation per card: saving and conflicts stay per card.
const detailsSave = useAppConfigurationPatch(vendor, appId, configuration.data);
const sourceSave = useAppConfigurationPatch(vendor, appId, configuration.data);
const networkSave = useAppConfigurationPatch(vendor, appId, configuration.data, {
  handledCodes: ["PROXY_REDACTED_MISMATCH"],
});

const providers = useProviders();
const providerName = computed(() => {
  const item = providers.data.value?.items.find((entry) => entry.key === provider);
  return item ? localized(item.name) : provider;
});
</script>

<template>
  <AsyncState
    :loading="configuration.isPending.value"
    :error="configuration.error.value"
    @retry="configuration.refetch()"
  >
    <div class="flex flex-col gap-6">
      <TemplateMissingAlert :missing="data?.template_missing" />

      <OverlaySectionCard
        v-slot="{ section }"
        :title="t('directory.sections.details')"
        :configuration="data"
        :reloading="reloading"
        :paths="DETAILS_PATHS"
        :schema="detailsSchema"
        :save="detailsSave"
        :read-only="readOnly"
      >
        <dl class="grid w-fit grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
          <dt class="text-muted">{{ t("directory.fields.appKey") }}</dt>
          <dd class="font-mono">{{ app.key }}</dd>
          <dt class="text-muted">{{ t("directory.fields.provider") }}</dt>
          <dd>{{ providerName }}</dd>
          <template v-if="data?.template_provider">
            <dt class="text-muted">{{ t("configuration.templateProvider") }}</dt>
            <dd>{{ data.template_provider }}</dd>
          </template>
        </dl>
        <p class="text-xs text-muted">{{ t("directory.fields.fixedAfterCreate") }}</p>
        <p v-if="section.linked.value" class="text-sm text-muted">
          {{ t("configuration.linkedHint") }}
        </p>
        <LocalizedTextFields
          :disabled="readOnly"
          :reset-binding="section.resetBinding"
          @reset="section.reset"
        />
        <div class="max-w-xs">
          <IconField
            :model-value="section.form.values.icon"
            :label="t('directory.icon.appIcon')"
            :empty-text="t('directory.icon.none')"
            :disabled="readOnly"
            @update:model-value="(value: string) => section.form.setFieldValue('icon', value)"
          >
            <template #reset>
              <FieldReset
                v-bind="section.resetBinding('icon')"
                :disabled="readOnly"
                @reset="section.reset('icon')"
              />
            </template>
          </IconField>
        </div>
      </OverlaySectionCard>

      <OverlaySectionCard
        v-if="SOURCE_PATHS.length > 0"
        v-slot="{ section }"
        :title="t('directory.sections.source')"
        :description="t('directory.source.epochHint')"
        :configuration="data"
        :reloading="reloading"
        :paths="SOURCE_PATHS"
        :schema="sourceSchema"
        :save="sourceSave"
        :read-only="readOnly"
      >
        <SourceFields
          :provider="provider"
          :disabled="readOnly"
          :reset-binding="section.resetBinding"
          @reset="section.reset"
        />
      </OverlaySectionCard>

      <ProxyCard
        :configuration="data"
        :reloading="reloading"
        :save="networkSave"
        :read-only="readOnly"
      />
    </div>
  </AsyncState>
</template>
