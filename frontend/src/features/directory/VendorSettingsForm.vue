<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { z } from "zod";
import {
  FieldReset,
  isEmptyPatch,
  OverlayFormActions,
  TemplateMissingAlert,
  useOverlayForm,
  useVendorConfiguration,
  useVendorConfigurationPatch,
  type VendorConfigurationPatch,
} from "@/features/configuration";
import { ProxyFields } from "@/features/proxy";
import { isApiError } from "@/shared/api";
import { toast } from "@/shared/lib";
import { AsyncState, Card, RevisionConflictAlert } from "@/shared/ui";
import EnabledSwitch from "./EnabledSwitch.vue";
import IconField from "./IconField.vue";
import LocalizedTextFields from "./LocalizedTextFields.vue";
import type { Vendor } from "./queries";
import { descriptionSchema, iconSchema, nameSchema, proxySchema } from "./schemas";

/** Vendor presentation and proxy (the vendor configuration overlay). */
const props = defineProps<{ vendor: Vendor }>();
const { t } = useI18n();

const PATHS = [
  "name.en",
  "name.zh-CN",
  "description.en",
  "description.zh-CN",
  "icon",
  "localized_icons.en",
  "localized_icons.zh-CN",
  "proxy",
] as const;

const schema = z.object({
  name: nameSchema,
  description: descriptionSchema,
  icon: iconSchema,
  localized_icons: z.object({ en: iconSchema, "zh-CN": iconSchema }),
  proxy: proxySchema,
});

const configuration = useVendorConfiguration(() => props.vendor.id);
const overlay = useOverlayForm({
  configuration: configuration.data,
  paths: PATHS,
  schema,
});
const save = useVendorConfigurationPatch(() => props.vendor.id, configuration.data, {
  handledCodes: ["PROXY_REDACTED_MISMATCH"],
});
const { form } = overlay;
const [icon] = form.defineField("icon");
const [iconEn] = form.defineField("localized_icons.en");
const [iconZh] = form.defineField("localized_icons.zh-CN");
const [proxy] = form.defineField("proxy");
const readOnly = computed(() => props.vendor.deleted_at !== null);
// The proxy URL is a nested value of one field; its errors are shown here.
const proxyServerError = ref<string>();
watch(proxy, () => (proxyServerError.value = undefined), { deep: true });
const proxyError = computed(() => {
  if (proxyServerError.value) return proxyServerError.value;
  const value = proxy.value;
  const missing = value.mode === "url" && !value.url?.trim();
  return form.submitCount.value > 0 && missing ? t("directory.validation.proxyUrl") : undefined;
});

const submit = form.handleSubmit(async (values) => {
  const patch = overlay.patch(values);
  if (isEmptyPatch(patch)) {
    toast({ title: t("configuration.nothingToSave") });
    return;
  }
  try {
    const saved = await save.mutateAsync(patch as VendorConfigurationPatch);
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
            <dl class="text-sm">
              <dt class="text-muted">{{ t("directory.fields.vendorId") }}</dt>
              <dd class="font-mono">{{ vendor.id }}</dd>
            </dl>
            <EnabledSwitch :entity="vendor" />
          </div>
          <p v-if="overlay.linked.value" class="text-sm text-muted">
            {{ t("configuration.linkedHint") }}
          </p>
          <LocalizedTextFields
            :disabled="readOnly"
            :reset-binding="overlay.resetBinding"
            @reset="overlay.reset"
          />
        </div>
      </Card>

      <Card :title="t('directory.sections.logos')" :description="t('directory.icon.vendorHint')">
        <fieldset class="grid gap-4 sm:grid-cols-3" :disabled="readOnly">
          <legend class="sr-only">{{ t("directory.sections.logos") }}</legend>
          <IconField
            v-model="icon"
            :label="t('directory.icon.defaultLogo')"
            :empty-text="t('directory.icon.none')"
            variant="logo"
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
          <IconField
            v-model="iconEn"
            :label="t('directory.icon.englishLogo')"
            :empty-text="t('directory.icon.usesDefault')"
            variant="logo"
            :disabled="readOnly"
          >
            <template #reset>
              <FieldReset
                v-bind="overlay.resetBinding('localized_icons.en')"
                @reset="overlay.reset('localized_icons.en')"
              />
            </template>
          </IconField>
          <IconField
            v-model="iconZh"
            :label="t('directory.icon.chineseLogo')"
            :empty-text="t('directory.icon.usesDefault')"
            variant="logo"
            :disabled="readOnly"
          >
            <template #reset>
              <FieldReset
                v-bind="overlay.resetBinding('localized_icons.zh-CN')"
                @reset="overlay.reset('localized_icons.zh-CN')"
              />
            </template>
          </IconField>
        </fieldset>
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
        class="sticky bottom-0 z-sticky -mx-1 rounded-lg border border-border bg-surface px-4 py-3 shadow-md"
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
