<script setup lang="ts">
// Files tab of hosted applications.
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import { HostedFilesPanel } from "@/features/hosted";
import { useAdminApp } from "@/features/releases";
import { Alert, AsyncState, EmptyState } from "@/shared/ui";

const { t } = useI18n();
const route = useRoute();
const vendor = computed(() => String(route.params.vendor));
const app = computed(() => String(route.params.app));
const record = useAdminApp(vendor, app);
const deleted = computed(() => Boolean(record.data.value?.deleted_at));
</script>

<template>
  <AsyncState
    :loading="record.isPending.value"
    :error="record.error.value"
    @retry="record.refetch()"
  >
    <EmptyState
      v-if="record.data.value?.provider !== 'hosted'"
      :title="t('releases.page.unsupportedTitle')"
      :description="t('hosted.page.unsupported')"
    />
    <div v-else class="flex flex-col gap-6">
      <Alert v-if="deleted" tone="warning">{{ t("hosted.page.deleted") }}</Alert>
      <HostedFilesPanel :vendor="vendor" :app="app" :deleted="deleted" />
    </div>
  </AsyncState>
</template>
