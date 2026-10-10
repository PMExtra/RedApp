<script setup lang="ts">
// Package B: create a vendor (`createVendor`, `uploadIcon`).
import { useI18n } from "vue-i18n";
import { PagePlaceholder } from "@/shared/ui";

const { t } = useI18n();
</script>

<template>
  <PagePlaceholder :title="t('adminShell.titles.vendorNew')" />
</template>
