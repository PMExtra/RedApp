<script setup lang="ts">
import { computed, useId } from "vue";
import { useI18n } from "vue-i18n";
import { InfoTip, Spinner, Switch } from "@/shared/ui";
import { useAppEnabled, useVendorEnabled, type App, type Vendor } from "./queries";

/**
 * Compact enable switch of a vendor or application for the tab toolbar. It
 * saves at once (`updateVendor` / `updateApp`) and never touches unsaved form
 * fields; the explanation sits behind an info icon.
 */
const props = defineProps<{ entity: Vendor | App; disabled?: boolean }>();
const { t } = useI18n();
const id = useId();
const vendorEnabled = useVendorEnabled();
const appEnabled = useAppEnabled();
const pending = computed(() => vendorEnabled.isPending.value || appEnabled.isPending.value);
const hint = computed(() =>
  "provider" in props.entity ? t("directory.enabledHint.app") : t("directory.enabledHint.vendor"),
);

function change(enabled: boolean): void {
  if (pending.value || enabled === props.entity.enabled) return;
  const entity = props.entity;
  if ("provider" in entity) appEnabled.mutate({ app: entity, enabled });
  else vendorEnabled.mutate({ vendor: entity, enabled });
}
</script>

<template>
  <div class="flex items-center gap-2">
    <Switch
      :id="id"
      :model-value="entity.enabled"
      :disabled="disabled || pending || !!entity.deleted_at"
      :aria-describedby="`${id}-hint`"
      @update:model-value="change"
    />
    <label :for="id" class="text-sm font-medium">{{ t("directory.fields.enabled") }}</label>
    <span :id="`${id}-hint`" class="sr-only">{{ hint }}</span>
    <Spinner v-if="pending" size="sm" class="text-muted" />
    <InfoTip v-else :label="t('directory.enabledAbout')" :content="hint" />
  </div>
</template>
