<script setup lang="ts">
// Package D: recent failures (`listEvents`, cursor pages).
import { useI18n } from "vue-i18n";
import { PagePlaceholder } from "@/shared/ui";

const { t } = useI18n();
</script>

<template>
  <PagePlaceholder :title="t('adminShell.titles.events')" />
</template>
