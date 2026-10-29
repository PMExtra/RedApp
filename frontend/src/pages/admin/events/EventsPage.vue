<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { EventsPanel } from "@/features/events";
import { PageHeader } from "@/shared/ui";

const { t } = useI18n();
</script>

<template>
  <div class="flex flex-col gap-6">
    <PageHeader :title="t('adminShell.titles.events')" :description="t('eventsPage.description')" />
    <EventsPanel />
  </div>
</template>
