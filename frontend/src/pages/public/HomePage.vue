<script setup lang="ts">
// Package A (public site): pinned and popular applications (`getHome`).
import { useI18n } from "vue-i18n";
import { PagePlaceholder } from "@/shared/ui";

const { t } = useI18n();
</script>

<template>
  <PagePlaceholder :title="t('publicShell.titles.home')" />
</template>
