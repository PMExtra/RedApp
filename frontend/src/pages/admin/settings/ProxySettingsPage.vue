<script setup lang="ts">
// Package D: global upstream proxy (`getGlobalProxySettings`, `replaceGlobalProxySettings`) with ProxyFields.
import { useI18n } from "vue-i18n";
import { PagePlaceholder } from "@/shared/ui";

const { t } = useI18n();
</script>

<template>
  <PagePlaceholder :title="t('adminShell.titles.proxy')" />
</template>
