<script setup lang="ts">
// Package A (public site): `/all` and `/{vendor}` catalog (`listCatalog`, `getPublicVendor`).
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import { PagePlaceholder } from "@/shared/ui";

const { t } = useI18n();
const route = useRoute();
</script>

<template>
  <PagePlaceholder
    :title="route.params.vendor ? t('publicShell.titles.vendor') : t('publicShell.titles.catalog')"
  />
</template>
