<script setup lang="ts">
import { computed, useId } from "vue";
import { useI18n } from "vue-i18n";
import { Spinner, Switch } from "@/shared/ui";
import { useAppEnabled, useVendorEnabled, type App, type Vendor } from "./queries";

/**
 * Enable switch of a vendor or application. It saves at once (`updateVendor`
 * / `updateApp`) and never touches unsaved form fields.
 */
const props = defineProps<{ entity: Vendor | App; disabled?: boolean }>();
const { t } = useI18n();
const id = useId();
const vendorEnabled = useVendorEnabled();
const appEnabled = useAppEnabled();
const pending = computed(() => vendorEnabled.isPending.value || appEnabled.isPending.value);

function change(enabled: boolean): void {
  if (pending.value || enabled === props.entity.enabled) return;
  const entity = props.entity;
  if ("provider" in entity) appEnabled.mutate({ app: entity, enabled });
  else vendorEnabled.mutate({ vendor: entity, enabled });
}
</script>

<template>
  <div class="flex items-start gap-3">
    <Switch
      :id="id"
      :model-value="entity.enabled"
      :disabled="disabled || pending || !!entity.deleted_at"
      :aria-describedby="`${id}-hint`"
      @update:model-value="change"
    />
    <div class="flex flex-col gap-0.5">
      <label :for="id" class="text-sm font-medium">{{ t("directory.fields.enabled") }}</label>
      <p :id="`${id}-hint`" class="flex items-center gap-1.5 text-xs text-muted">
        <Spinner v-if="pending" size="sm" />
        {{
          "provider" in entity ? t("directory.enabledHint.app") : t("directory.enabledHint.vendor")
        }}
      </p>
    </div>
  </div>
</template>
