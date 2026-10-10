<script setup lang="ts">
import { computed, ref } from "vue";
import { RefreshCw } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import {
  GLOBAL_SCOPE,
  MetricCards,
  MetricHistoryDialog,
  useGlobalStatus,
  type Metric,
  type MetricKey,
} from "@/features/metrics";
import { describeError } from "@/shared/api";
import { useAutoRefresh } from "@/shared/lib";
import {
  Alert,
  AsyncState,
  AutoRefreshToggle,
  IconButton,
  PageHeader,
  RelativeTime,
  Skeleton,
} from "@/shared/ui";

const { t } = useI18n();
const auto = useAutoRefresh();
const status = useGlobalStatus({ refetchInterval: auto.refetchInterval });
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
    <PageHeader
      :title="t('adminShell.titles.overview')"
      :description="t('overviewPage.description')"
    >
      <template #meta>
        <span>
          {{ t("metrics.sampled") }}
          <RelativeTime :value="snapshot?.sampled_at" />
        </span>
        <span v-if="snapshot" aria-hidden="true">·</span>
        <span v-if="snapshot">
          {{ t("overviewPage.started") }}
          <RelativeTime :value="snapshot.started_at" />
        </span>
      </template>
      <template #actions>
        <AutoRefreshToggle v-model="auto.enabled.value" />
        <IconButton
          :label="t('common.actions.refresh')"
          variant="secondary"
          :loading="status.isFetching.value"
          @click="status.refetch()"
        >
          <RefreshCw aria-hidden="true" />
        </IconButton>
      </template>
    </PageHeader>

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
          <Skeleton v-for="index in 8" :key="index" class="h-24" />
        </div>
      </template>
      <MetricCards
        v-if="snapshot"
        :metrics="snapshot.metrics"
        :scope="GLOBAL_SCOPE"
        @select="selected = $event"
      />
    </AsyncState>

    <MetricHistoryDialog v-model:metric="selected" :scope="GLOBAL_SCOPE" />
  </div>
</template>
