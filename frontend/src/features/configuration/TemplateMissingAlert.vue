<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { Alert } from "@/shared/ui";

/** Shown when the preset a configuration is linked to no longer exists in this build. */
defineProps<{ missing: boolean | undefined }>();
const { t } = useI18n();
</script>

<template>
  <Alert v-if="missing" tone="warning">{{ t("configuration.templateMissing") }}</Alert>
</template>
