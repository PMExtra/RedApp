<script setup lang="ts">
// Package C: hosted files with upload/import progress (`listHostedFiles`, `uploadHostedFile`, `importHostedFile`, `getHostedTransfer`, `cancelHostedTransfer`, `deleteHostedFile`).
import { useI18n } from "vue-i18n";
import { PagePlaceholder } from "@/shared/ui";

const { t } = useI18n();
</script>

<template>
  <PagePlaceholder :title="t('adminShell.tabs.files')" />
</template>
