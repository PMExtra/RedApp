<script setup lang="ts">
// Package B: application table with sorting, toggles and delete (`listApps`, `updateApp`, `deleteApp`).
import { useI18n } from "vue-i18n";
import { PagePlaceholder } from "@/shared/ui";

const { t } = useI18n();
</script>

<template>
  <PagePlaceholder :title="t('adminShell.tabs.vendorApps')" />
</template>
