<script setup lang="ts">
// Versions tab of release applications: metrics, versions and stored files.
// AppLayout only shows it for codex and claude-code applications that are not deleted.
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import {
  APP_COMMON_METRICS,
  MetricCards,
  MetricHistoryDialog,
  appScope,
  useAppStatus,
  type Metric,
  type MetricKey,
} from "@/features/metrics";
import { ReleaseInventory } from "@/features/releases";
import { describeError } from "@/shared/api";
import { useAutoRefresh } from "@/shared/lib";
import { Alert, AsyncState, AutoRefreshToggle, RelativeTime, Skeleton } from "@/shared/ui";

const { t } = useI18n();
const route = useRoute();
const vendor = computed(() => String(route.params.vendor));
const app = computed(() => String(route.params.app));
const scope = computed(() => appScope(vendor.value, app.value));
const auto = useAutoRefresh();
const status = useAppStatus(vendor, app, { refetchInterval: auto.refetchInterval });
const snapshot = computed(() => status.data.value);
// A failed refresh keeps the last snapshot on screen with a warning.
const staleError = computed(() =>
  snapshot.value && status.error.value ? describeError(status.error.value) : null,
);

// Keep the key, so the dialog shows the newest value after each refresh.
const selectedKey = ref<MetricKey>();
const selected = computed<Metric | undefined>({
  get: () => snapshot.value?.metrics.find((metric) => metric.key === selectedKey.value),
  set: (metric) => {
    selectedKey.value = metric?.key;
  },
});
</script>

<template>
  <div class="flex flex-col gap-6">
    <div class="flex flex-wrap items-center justify-end gap-3">
      <span v-if="snapshot" class="text-xs text-muted">
        {{ t("metrics.sampled") }}
        <RelativeTime :value="snapshot.sampled_at" />
      </span>
      <AutoRefreshToggle v-model="auto.enabled.value" />
    </div>
    <Alert v-if="staleError" tone="warning" :title="t('metrics.stale')">
      {{ staleError.message }}
      <span v-if="staleError.requestId" class="mt-1 block text-xs text-muted">
        {{ t("common.requestId") }}: <code>{{ staleError.requestId }}</code>
      </span>
    </Alert>
    <AsyncState
      :loading="status.isPending.value && !status.error.value"
      :error="snapshot ? undefined : status.error.value"
      @retry="status.refetch()"
    >
      <template #loading>
        <div class="grid grid-cols-[repeat(auto-fill,minmax(13rem,1fr))] gap-3">
          <Skeleton v-for="index in 4" :key="index" class="h-24" />
        </div>
      </template>
      <MetricCards
        v-if="snapshot"
        :metrics="snapshot.metrics"
        :primary="APP_COMMON_METRICS"
        :scope="scope"
        @select="selected = $event"
      />
    </AsyncState>
    <MetricHistoryDialog v-model:metric="selected" :scope="scope" />
    <ReleaseInventory :vendor="vendor" :app="app" :auto-refresh="auto.enabled.value" />
  </div>
</template>
