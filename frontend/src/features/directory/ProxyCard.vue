<script setup lang="ts" generic="Patch">
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { z } from "zod";
import {
  FieldReset,
  OverlaySectionCard,
  type OverlayConfiguration,
  type SectionSave,
} from "@/features/configuration";
import {
  ProxyFields,
  RedirectProtectionFields,
  type ProxyValue,
  type RedirectProtectionMode,
} from "@/features/proxy";
import { isApiError, type Schema } from "@/shared/api";
import RedirectAllowlist from "./RedirectAllowlist.vue";
import { proxySchema, redirectAllowlistSchema, redirectProtectionSchema } from "./schemas";

/**
 * The network card of vendor and application settings: the proxy and
 * redirect protection overrides and, for an HTTP cache (`allowlist`), its
 * redirect allowlist, saved together. The mutation must handle
 * `PROXY_REDACTED_MISMATCH`, which is shown at the proxy URL.
 */
const props = defineProps<{
  configuration:
    | (OverlayConfiguration & {
        proxy_effective?: Schema<"ProxyEffective">;
        redirect_protection_effective?: Schema<"RedirectProtectionEffective">;
      })
    | undefined;
  reloading?: boolean;
  save: SectionSave<Patch>;
  readOnly?: boolean;
  allowlist?: boolean;
}>();
const { t } = useI18n();
const schema = z.object({
  proxy: proxySchema,
  redirect_protection: redirectProtectionSchema,
  redirect_allowlist: redirectAllowlistSchema.optional(),
});
const paths = computed(() =>
  props.allowlist
    ? ["proxy", "redirect_protection", "redirect_allowlist"]
    : ["proxy", "redirect_protection"],
);
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
    :paths="paths"
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
    <RedirectProtectionFields
      :model-value="section.form.values.redirect_protection"
      allow-inherit
      :effective="configuration?.redirect_protection_effective"
      :disabled="readOnly"
      @update:model-value="
        (value: RedirectProtectionMode) => section.form.setFieldValue('redirect_protection', value)
      "
    >
      <template #reset>
        <FieldReset
          v-bind="section.resetBinding('redirect_protection')"
          :disabled="readOnly"
          @reset="section.reset('redirect_protection')"
        />
      </template>
    </RedirectProtectionFields>
    <RedirectAllowlist v-if="allowlist" :disabled="readOnly">
      <template #reset>
        <FieldReset
          v-bind="section.resetBinding('redirect_allowlist')"
          :disabled="readOnly"
          @reset="section.reset('redirect_allowlist')"
        />
      </template>
    </RedirectAllowlist>
  </OverlaySectionCard>
</template>
