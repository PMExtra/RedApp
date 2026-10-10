<script setup lang="ts">
import { useI18n } from "vue-i18n";
import Alert from "./Alert.vue";
import PageHeader from "./PageHeader.vue";

/**
 * Temporary body of routes whose feature package has not landed yet.
 * Delete this component (and `ui.placeholder`) once no page uses it.
 */
defineProps<{ title: string }>();
const { t } = useI18n();
</script>

<template>
  <div class="flex flex-col gap-6">
    <PageHeader :title="title" />
    <Alert tone="info">{{ t("ui.placeholder") }}</Alert>
  </div>
</template>
