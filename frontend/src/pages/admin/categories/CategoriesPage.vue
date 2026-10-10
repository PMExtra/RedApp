<script setup lang="ts">
// Package B: category names (`listCategories`, `getCategory`, `patchCategory`).
import { useI18n } from "vue-i18n";
import { PagePlaceholder } from "@/shared/ui";

const { t } = useI18n();
</script>

<template>
  <PagePlaceholder :title="t('adminShell.titles.categories')" />
</template>
