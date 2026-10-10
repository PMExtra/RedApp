<script setup lang="ts">
// Package A (public site): application details (`getPublicApp`), the sandboxed
// InstructionsDocument and hosted downloads (`listPublicHostedFiles`).
import { useI18n } from "vue-i18n";
import { PagePlaceholder } from "@/shared/ui";

const { t } = useI18n();
</script>

<template>
  <PagePlaceholder :title="t('publicShell.titles.app')" />
</template>
