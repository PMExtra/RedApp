<script setup lang="ts">
// Package D: global status metrics and history charts (`getStatus`, `getHistory`).
import { useI18n } from "vue-i18n";
import { PagePlaceholder } from "@/shared/ui";

const { t } = useI18n();
</script>

<template>
  <PagePlaceholder :title="t('adminShell.titles.overview')" />
</template>
