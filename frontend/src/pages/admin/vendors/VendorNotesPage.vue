<script setup lang="ts">
// Package B: vendor admin notes (`getVendorNotes`, `replaceVendorNotes`).
import { useI18n } from "vue-i18n";
import { PagePlaceholder } from "@/shared/ui";

const { t } = useI18n();
</script>

<template>
  <PagePlaceholder :title="t('adminShell.tabs.adminNotes')" />
</template>
