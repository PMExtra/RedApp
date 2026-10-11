<script setup lang="ts" generic="Patch">
import { ref } from "vue";
import { useI18n } from "vue-i18n";
import { z } from "zod";
import {
  FieldReset,
  OverlaySectionCard,
  type OverlayConfiguration,
  type SectionSave,
} from "@/features/configuration";
import { ProxyFields, type ProxyValue } from "@/features/proxy";
import { isApiError, type Schema } from "@/shared/api";
import { proxySchema } from "./schemas";

/**
 * The network card of vendor and application settings: the proxy override,
 * saved on its own. The mutation must handle `PROXY_REDACTED_MISMATCH`, which
 * is shown at the proxy URL.
 */
const props = defineProps<{
  configuration:
    (OverlayConfiguration & { proxy_effective?: Schema<"ProxyEffective"> }) | undefined;
  reloading?: boolean;
  save: SectionSave<Patch>;
  readOnly?: boolean;
}>();
const { t } = useI18n();
const schema = z.object({ proxy: proxySchema });
// The proxy URL is a nested value of one field; its errors are shown here.
const serverError = ref<string>();

function urlError(value: ProxyValue, submitted: boolean): string | undefined {
  if (serverError.value) return serverError.value;
  const missing = value.mode === "url" && !value.url?.trim();
  return submitted && missing ? t("directory.validation.proxyUrl") : undefined;
}

function onError(error: unknown): void {
  if (isApiError(error, "PROXY_REDACTED_MISMATCH")) {
    serverError.value = t("errors.codes.PROXY_REDACTED_MISMATCH");
  }
}
</script>

<template>
  <OverlaySectionCard
    v-slot="{ section }"
    :title="t('directory.sections.network')"
    :configuration="configuration"
    :reloading="reloading"
    :paths="['proxy']"
    :schema="schema"
    :save="props.save"
    :read-only="readOnly"
    @error="onError"
  >
    <ProxyFields
      :model-value="section.form.values.proxy"
      allow-inherit
      :effective="configuration?.proxy_effective"
      :url-error="urlError(section.form.values.proxy, section.form.submitCount.value > 0)"
      :disabled="readOnly"
      @update:model-value="
        (value: ProxyValue) => {
          serverError = undefined;
          section.form.setFieldValue('proxy', value);
        }
      "
    >
      <template #reset>
        <FieldReset
          v-bind="section.resetBinding('proxy')"
          :disabled="readOnly"
          @reset="section.reset('proxy')"
        />
      </template>
    </ProxyFields>
  </OverlaySectionCard>
</template>
