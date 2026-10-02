<script setup lang="ts">
import { t } from "../i18n";
defineProps<{
  label: string;
  page: number;
  previous: boolean;
  next: boolean;
  loading: boolean;
}>();
const emit = defineEmits<{ previous: []; next: []; refresh: [] }>();
</script>
<template>
  <nav class="form-actions page-navigation" :aria-label="label">
    <button
      class="secondary"
      :disabled="loading || !previous"
      @click="emit('previous')"
    >
      {{ t("Previous page") }}</button
    ><span>{{ t("Page {page}", { page }) }}</span
    ><button
      class="secondary"
      :disabled="loading || !next"
      @click="emit('next')"
    >
      {{ t("Next page") }}</button
    ><button class="secondary" :disabled="loading" @click="emit('refresh')">
      {{ loading ? t("Refreshing…") : t("Refresh") }}
    </button>
  </nav>
</template>
