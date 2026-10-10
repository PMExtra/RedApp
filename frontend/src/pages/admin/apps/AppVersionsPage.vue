<script setup lang="ts">
// Versions tab of release applications: metrics, versions and stored files.
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import { AppMetricsPanel, ReleaseInventory, hasVersions, useAdminApp } from "@/features/releases";
import { useAutoRefresh } from "@/shared/lib";
import { Alert, AsyncState, AutoRefreshToggle, EmptyState } from "@/shared/ui";

const { t } = useI18n();
const route = useRoute();
const vendor = computed(() => String(route.params.vendor));
const app = computed(() => String(route.params.app));
const record = useAdminApp(vendor, app);
const auto = useAutoRefresh();
</script>

<template>
  <AsyncState
    :loading="record.isPending.value"
    :error="record.error.value"
    @retry="record.refetch()"
  >
    <EmptyState
      v-if="!hasVersions(record.data.value?.provider)"
      :title="t('releases.page.unsupportedTitle')"
      :description="t('releases.page.unsupportedVersions')"
    />
    <div v-else class="flex flex-col gap-6">
      <Alert v-if="record.data.value?.deleted_at" tone="warning">
        {{ t("releases.page.deleted") }}
      </Alert>
      <div class="flex justify-end">
        <AutoRefreshToggle v-model="auto.enabled.value" />
      </div>
      <AppMetricsPanel :vendor="vendor" :app="app" :auto-refresh="auto.enabled.value" />
      <ReleaseInventory :vendor="vendor" :app="app" :auto-refresh="auto.enabled.value" />
    </div>
  </AsyncState>
</template>
