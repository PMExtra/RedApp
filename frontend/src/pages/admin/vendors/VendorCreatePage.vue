<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { VendorCreateForm } from "@/features/directory";
import { PageHeader, type Crumb } from "@/shared/ui";

const { t } = useI18n();
const breadcrumbs = computed<Crumb[]>(() => [
  { label: t("adminShell.titles.vendors"), to: { name: "admin-vendors" } },
  { label: t("adminShell.titles.vendorNew") },
]);
</script>

<template>
  <div class="flex flex-col gap-6">
    <PageHeader
      :title="t('adminShell.titles.vendorNew')"
      :description="t('directory.create.vendorDescription')"
      :breadcrumbs="breadcrumbs"
    />
    <VendorCreateForm />
  </div>
</template>
