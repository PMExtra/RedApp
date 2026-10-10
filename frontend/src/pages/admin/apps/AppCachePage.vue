<script setup lang="ts">
// Package C: prewarm, retention, version cleanup or HTTP cache entries, refresh and cleanup (prewarm, retention, version-cleanup and cache operations, `listSources`, `getAutoCleanupStatus`).
import { useI18n } from "vue-i18n";
import { PagePlaceholder } from "@/shared/ui";

const { t } = useI18n();
</script>

<template>
  <PagePlaceholder :title="t('adminShell.tabs.cache')" />
</template>
