<script setup lang="ts">
// Events tab of every application (deleted ones too).
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import { EventsPanel } from "@/features/events";

const { t } = useI18n();
const route = useRoute();
const app = computed(() => ({
  vendor: String(route.params.vendor),
  app: String(route.params.app),
}));
</script>

<template>
  <div class="flex flex-col gap-4">
    <p class="text-sm text-muted">{{ t("appEventsPage.description") }}</p>
    <EventsPanel :app="app" />
  </div>
</template>
