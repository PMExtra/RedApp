<script setup lang="ts">
// Package B: vendor configuration overlay, enable switch, delete (`getVendor`, `getVendorConfiguration`, `patchVendorConfiguration`, `updateVendor`, `deleteVendor`).
import { useI18n } from "vue-i18n";
import { PagePlaceholder } from "@/shared/ui";

const { t } = useI18n();
</script>

<template>
  <PagePlaceholder :title="t('adminShell.tabs.vendorSettings')" />
</template>
