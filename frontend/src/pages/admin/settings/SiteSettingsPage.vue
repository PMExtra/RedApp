<script setup lang="ts">
// Package D: site texts, public URL and homepage pins (`getSiteSettings`, `getPublicUrlSettings`, `getHomepageSettings` and their PUTs).
import { useI18n } from "vue-i18n";
import { PagePlaceholder } from "@/shared/ui";

const { t } = useI18n();
</script>

<template>
  <PagePlaceholder :title="t('adminShell.titles.site')" />
</template>
