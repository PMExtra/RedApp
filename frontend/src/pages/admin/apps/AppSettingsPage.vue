<script setup lang="ts">
// Package B: application configuration overlay (details, taxonomy, instructions); renders the cache policy and channel sections of package C (`getAppConfiguration`, `patchAppConfiguration`, `updateApp`, `deleteApp`, `listCategories`).
import { useI18n } from "vue-i18n";
import { PagePlaceholder } from "@/shared/ui";

const { t } = useI18n();
</script>

<template>
  <PagePlaceholder :title="t('adminShell.tabs.appSettings')" />
</template>
