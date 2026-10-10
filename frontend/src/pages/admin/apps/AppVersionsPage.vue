<script setup lang="ts">
// Package C: status metrics, versions and resources (`getAppStatus`, `getAppHistory`, `listVersions`, `listResources`).
import { useI18n } from "vue-i18n";
import { PagePlaceholder } from "@/shared/ui";

const { t } = useI18n();
</script>

<template>
  <PagePlaceholder :title="t('adminShell.tabs.versions')" />
</template>
