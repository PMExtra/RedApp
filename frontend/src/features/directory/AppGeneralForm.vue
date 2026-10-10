<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { z } from "zod";
import {
  FieldReset,
  isEmptyPatch,
  OverlayFormActions,
  TemplateMissingAlert,
  useAppConfiguration,
  useAppConfigurationPatch,
  useOverlayForm,
  type AppConfigurationPatch,
} from "@/features/configuration";
import { ProxyFields } from "@/features/proxy";
import { isApiError } from "@/shared/api";
import { useLocalized } from "@/shared/i18n";
import { toast } from "@/shared/lib";
import { AsyncState, Card, RevisionConflictAlert } from "@/shared/ui";
import EnabledSwitch from "./EnabledSwitch.vue";
import IconField from "./IconField.vue";
import { isReleaseProvider } from "./links";
import LocalizedTextFields from "./LocalizedTextFields.vue";
import { useProviders, type App } from "./queries";
import {
  baseUrlsSchema,
  descriptionSchema,
  httpUrlSchema,
  iconSchema,
  nameSchema,
  proxySchema,
  strategySchema,
  ttlSchema,
} from "./schemas";
import SourceFields from "./SourceFields.vue";

/**
 * Application presentation, upstream and proxy (one form over the
 * application configuration overlay). Only edited or reset paths are saved.
 */
const props = defineProps<{ app: App }>();
const { t } = useI18n();
const localized = useLocalized();
const provider = props.app.provider;

const SOURCE_PATHS =
  provider === "http-cache"
    ? (["base_urls", "source_strategy", "cache_ttl_seconds"] as const)
    : isReleaseProvider(provider)
      ? (["base_url"] as const)
      : ([] as const);
const PATHS = [
  "name.en",
  "name.zh-CN",
  "description.en",
  "description.zh-CN",
  "icon",
  "proxy",
  ...SOURCE_PATHS,
];

const schema = z.object({
  name: nameSchema,
  description: descriptionSchema,
  icon: iconSchema,
  proxy: proxySchema,
  ...(provider === "http-cache"
    ? { base_urls: baseUrlsSchema, source_strategy: strategySchema, cache_ttl_seconds: ttlSchema }
    : {}),
  ...(isReleaseProvider(provider) ? { base_url: httpUrlSchema } : {}),
});

const configuration = useAppConfiguration(
  () => props.app.vendor_id,
  () => props.app.id,
);
const overlay = useOverlayForm({ configuration: configuration.data, paths: PATHS, schema });
const save = useAppConfigurationPatch(
  () => props.app.vendor_id,
  () => props.app.id,
  configuration.data,
  { handledCodes: ["PROXY_REDACTED_MISMATCH"] },
);
const { form } = overlay;
const [icon] = form.defineField("icon");
const [proxy] = form.defineField("proxy");
const readOnly = computed(() => props.app.deleted_at !== null);
// The proxy URL is a nested value of one field; its errors are shown here.
const proxyServerError = ref<string>();
watch(proxy, () => (proxyServerError.value = undefined), { deep: true });
const proxyError = computed(() => {
  if (proxyServerError.value) return proxyServerError.value;
  const value = proxy.value;
  const missing = value.mode === "url" && !value.url?.trim();
  return form.submitCount.value > 0 && missing ? t("directory.validation.proxyUrl") : undefined;
});
const providers = useProviders();
const providerName = computed(() => {
  const item = providers.data.value?.items.find((entry) => entry.key === provider);
  return item ? localized(item.name) : provider;
});

const submit = form.handleSubmit(async (values) => {
  const patch = overlay.patch(values);
  if (isEmptyPatch(patch)) {
    toast({ title: t("configuration.nothingToSave") });
    return;
  }
  try {
    const saved = await save.mutateAsync(patch as AppConfigurationPatch);
    overlay.load(saved);
    toast({ tone: "success", title: t("configuration.saved") });
  } catch (error) {
    if (isApiError(error, "PROXY_REDACTED_MISMATCH")) {
      proxyServerError.value = t("errors.codes.PROXY_REDACTED_MISMATCH");
    }
  }
});
</script>

<template>
  <AsyncState
    :loading="configuration.isPending.value"
    :error="configuration.error.value"
    @retry="configuration.refetch()"
  >
    <form class="flex flex-col gap-6" novalidate @submit.prevent="submit">
      <TemplateMissingAlert :missing="configuration.data.value?.template_missing" />
      <RevisionConflictAlert
        v-if="save.hasConflict.value"
        :reloading="configuration.isFetching.value"
        @reload="save.reload()"
      />

      <Card :title="t('directory.sections.details')">
        <div class="flex flex-col gap-5">
          <div class="flex flex-wrap items-start justify-between gap-4">
            <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
              <dt class="text-muted">{{ t("directory.fields.appKey") }}</dt>
              <dd class="font-mono">{{ app.key }}</dd>
              <dt class="text-muted">{{ t("directory.fields.provider") }}</dt>
              <dd>{{ providerName }}</dd>
            </dl>
            <EnabledSwitch :entity="app" />
          </div>
          <p class="text-xs text-muted">{{ t("directory.fields.fixedAfterCreate") }}</p>
          <p v-if="overlay.linked.value" class="text-sm text-muted">
            {{ t("configuration.linkedHint") }}
          </p>
          <LocalizedTextFields
            :disabled="readOnly"
            :reset-binding="overlay.resetBinding"
            @reset="overlay.reset"
          />
          <div class="max-w-xs">
            <IconField
              v-model="icon"
              :label="t('directory.icon.appIcon')"
              :empty-text="t('directory.icon.none')"
              :disabled="readOnly"
            >
              <template #reset>
                <FieldReset
                  v-bind="overlay.resetBinding('icon')"
                  :disabled="readOnly"
                  @reset="overlay.reset('icon')"
                />
              </template>
            </IconField>
          </div>
        </div>
      </Card>

      <Card
        v-if="SOURCE_PATHS.length > 0"
        :title="t('directory.sections.source')"
        :description="t('directory.source.epochHint')"
      >
        <SourceFields
          :provider="provider"
          :disabled="readOnly"
          :reset-binding="overlay.resetBinding"
          @reset="overlay.reset"
        />
      </Card>

      <Card :title="t('directory.sections.network')">
        <div class="flex items-start gap-2">
          <div class="min-w-0 flex-1">
            <ProxyFields
              v-model="proxy"
              allow-inherit
              :effective="configuration.data.value?.proxy_effective"
              :url-error="proxyError"
              :disabled="readOnly"
            />
          </div>
          <FieldReset
            v-bind="overlay.resetBinding('proxy')"
            :disabled="readOnly"
            @reset="overlay.reset('proxy')"
          />
        </div>
      </Card>

      <div
        v-if="!readOnly"
        class="sticky bottom-0 z-sticky rounded-lg border border-border bg-surface px-4 py-3 shadow-md"
      >
        <OverlayFormActions
          :dirty="overlay.dirty.value"
          :saving="save.isPending.value"
          @discard="overlay.discard()"
        />
      </div>
    </form>
  </AsyncState>
</template>
