<script setup lang="ts">
// Package B: application admin notes (`getAppNotes`, `replaceAppNotes`).
import { useI18n } from "vue-i18n";
import { PagePlaceholder } from "@/shared/ui";

const { t } = useI18n();
</script>

<template>
  <PagePlaceholder :title="t('adminShell.tabs.adminNotes')" />
</template>
