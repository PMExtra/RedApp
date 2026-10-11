<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { z } from "zod";
import {
  FieldReset,
  OverlaySectionCard,
  TemplateMissingAlert,
  useVendorConfiguration,
  useVendorConfigurationPatch,
} from "@/features/configuration";
import { AsyncState } from "@/shared/ui";
import IconField from "./IconField.vue";
import LocalizedTextFields from "./LocalizedTextFields.vue";
import ProxyCard from "./ProxyCard.vue";
import type { Vendor } from "./queries";
import { descriptionSchema, iconSchema, nameSchema } from "./schemas";

/**
 * Vendor details, logos and proxy: three cards over the vendor configuration
 * overlay, each saved on its own.
 */
const props = defineProps<{ vendor: Vendor }>();
const { t } = useI18n();
const vendorId = () => props.vendor.id;

const DETAILS_PATHS = ["name.en", "name.zh-CN", "description.en", "description.zh-CN"];
const detailsSchema = z.object({ name: nameSchema, description: descriptionSchema });
const LOGO_PATHS = ["icon", "localized_icons.en", "localized_icons.zh-CN"];
const logosSchema = z.object({
  icon: iconSchema,
  localized_icons: z.object({ en: iconSchema, "zh-CN": iconSchema }),
});

const configuration = useVendorConfiguration(vendorId);
const data = computed(() => configuration.data.value);
const reloading = computed(() => configuration.isFetching.value);
const readOnly = computed(() => props.vendor.deleted_at !== null);
// One mutation per card: saving and conflicts stay per card.
const detailsSave = useVendorConfigurationPatch(vendorId, configuration.data);
const logosSave = useVendorConfigurationPatch(vendorId, configuration.data);
const networkSave = useVendorConfigurationPatch(vendorId, configuration.data, {
  handledCodes: ["PROXY_REDACTED_MISMATCH"],
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
        <dl class="text-sm">
          <dt class="text-muted">{{ t("directory.fields.vendorId") }}</dt>
          <dd class="font-mono">{{ vendor.id }}</dd>
        </dl>
        <p v-if="section.linked.value" class="text-sm text-muted">
          {{ t("configuration.linkedHint") }}
        </p>
        <LocalizedTextFields
          :disabled="readOnly"
          :reset-binding="section.resetBinding"
          @reset="section.reset"
        />
      </OverlaySectionCard>

      <OverlaySectionCard
        v-slot="{ section }"
        :title="t('directory.sections.logos')"
        :description="t('directory.icon.vendorHint')"
        :configuration="data"
        :reloading="reloading"
        :paths="LOGO_PATHS"
        :schema="logosSchema"
        :save="logosSave"
        :read-only="readOnly"
      >
        <fieldset class="grid gap-4 sm:grid-cols-3" :disabled="readOnly">
          <legend class="sr-only">{{ t("directory.sections.logos") }}</legend>
          <IconField
            :model-value="section.form.values.icon"
            :label="t('directory.icon.defaultLogo')"
            :empty-text="t('directory.icon.none')"
            variant="logo"
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
          <IconField
            :model-value="section.form.values.localized_icons.en"
            :label="t('directory.icon.englishLogo')"
            :empty-text="t('directory.icon.usesDefault')"
            variant="logo"
            :disabled="readOnly"
            @update:model-value="
              (value: string) => section.form.setFieldValue('localized_icons.en', value)
            "
          >
            <template #reset>
              <FieldReset
                v-bind="section.resetBinding('localized_icons.en')"
                :disabled="readOnly"
                @reset="section.reset('localized_icons.en')"
              />
            </template>
          </IconField>
          <IconField
            :model-value="section.form.values.localized_icons['zh-CN']"
            :label="t('directory.icon.chineseLogo')"
            :empty-text="t('directory.icon.usesDefault')"
            variant="logo"
            :disabled="readOnly"
            @update:model-value="
              (value: string) => section.form.setFieldValue('localized_icons.zh-CN', value)
            "
          >
            <template #reset>
              <FieldReset
                v-bind="section.resetBinding('localized_icons.zh-CN')"
                :disabled="readOnly"
                @reset="section.reset('localized_icons.zh-CN')"
              />
            </template>
          </IconField>
        </fieldset>
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
