<script setup lang="ts">
// Package B: vendor grid with search, state filter and import (`listVendors`, `listApps`, `previewImport`, `executeImport`).
import { useI18n } from "vue-i18n";
import { PagePlaceholder } from "@/shared/ui";

const { t } = useI18n();
</script>

<template>
  <PagePlaceholder :title="t('adminShell.titles.vendors')" />
</template>
