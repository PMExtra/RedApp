<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRoute } from "vue-router";
import { AppCreateForm, useVendor } from "@/features/directory";
import { useLocalized } from "@/shared/i18n";
import { AsyncState, Button, EmptyState, PageHeader, type Crumb } from "@/shared/ui";

const { t } = useI18n();
const localized = useLocalized();
const route = useRoute();
const vendorId = computed(() => String(route.params.vendor));
const vendor = useVendor(vendorId);
const vendorName = computed(() => {
  const data = vendor.data.value;
  return data ? localized(data.name) || data.id : vendorId.value;
});
const breadcrumbs = computed<Crumb[]>(() => [
  { label: t("adminShell.titles.vendors"), to: { name: "admin-vendors" } },
  {
    label: vendorName.value,
    to: { name: "admin-vendor-apps", params: { vendor: vendorId.value } },
  },
  { label: t("adminShell.titles.appNew") },
]);
</script>

<template>
  <div class="flex flex-col gap-6">
    <PageHeader
      :title="t('adminShell.titles.appNew')"
      :description="t('directory.create.appDescription', { vendor: vendorName })"
      :breadcrumbs="breadcrumbs"
    />
    <AsyncState
      :loading="vendor.isPending.value"
      :error="vendor.error.value"
      @retry="vendor.refetch()"
    >
      <EmptyState v-if="vendor.data.value?.deleted_at" :title="t('directory.create.vendorDeleted')">
        <template #action>
          <Button as-child>
            <RouterLink :to="{ name: 'admin-vendors' }">{{ t("directory.backToList") }}</RouterLink>
          </Button>
        </template>
      </EmptyState>
      <AppCreateForm v-else :vendor="vendorId" />
    </AsyncState>
  </div>
</template>
