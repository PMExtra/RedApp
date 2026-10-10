<script setup lang="ts">
// Cache tab: prewarm, retention and version cleanup of release applications;
// cached files, refresh, cleanup and prewarm of HTTP cache applications.
// AppLayout only shows it for those providers and marks deleted applications.
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import {
  AutoCleanupStatus,
  CacheCleanupPanel,
  CacheEntriesPanel,
  CacheRefreshPanel,
} from "@/features/http-cache";
import { hasVersions, useApp } from "@/features/directory";
import { PrewarmPanel } from "@/features/prewarm";
import { SourceEpochSelect, VersionCleanup, useSources } from "@/features/releases";
import { RetentionPanel } from "@/features/retention";
import { AsyncState, Tabs, type TabItem } from "@/shared/ui";

const { t } = useI18n();
const route = useRoute();
const vendor = computed(() => String(route.params.vendor));
const app = computed(() => String(route.params.app));
const record = useApp(vendor, app);
const provider = computed(() => record.data.value?.provider);
const deleted = computed(() => Boolean(record.data.value?.deleted_at));

// HTTP cache: one source epoch selection for the file list and cleanup.
const sourceEpoch = ref<number | null>(null);
watch([vendor, app], () => {
  sourceEpoch.value = null;
});
const sources = useSources(vendor, app);
const canRefresh = computed(() => {
  const current = sources.data.value?.items.find((source) => source.current);
  return (
    sourceEpoch.value === null &&
    !deleted.value &&
    record.data.value?.enabled === true &&
    current?.active === true
  );
});
const section = ref("files");
const sections = computed<TabItem[]>(() => [
  { value: "files", label: t("httpCache.sections.files") },
  { value: "maintenance", label: t("httpCache.sections.maintenance") },
  { value: "prewarm", label: t("httpCache.sections.prewarm") },
]);
</script>

<template>
  <AsyncState
    :loading="record.isPending.value"
    :error="record.error.value"
    @retry="record.refetch()"
  >
    <div v-if="provider" class="flex flex-col gap-6">
      <template v-if="hasVersions(provider)">
        <PrewarmPanel :vendor="vendor" :app="app" :read-only="deleted" />
        <RetentionPanel :vendor="vendor" :app="app" :read-only="deleted" />
        <VersionCleanup :vendor="vendor" :app="app" :disabled="deleted" />
      </template>

      <template v-else>
        <SourceEpochSelect v-model="sourceEpoch" class="max-w-2xl" :vendor="vendor" :app="app" />
        <Tabs v-model="section" :items="sections" :label="t('httpCache.sections.label')">
          <template #files>
            <div class="flex flex-col gap-6">
              <CacheEntriesPanel
                :vendor="vendor"
                :app="app"
                :source-epoch="sourceEpoch"
                :can-refresh="canRefresh"
              />
              <AutoCleanupStatus />
            </div>
          </template>
          <template #maintenance>
            <div class="flex flex-col gap-6">
              <CacheRefreshPanel :vendor="vendor" :app="app" :available="canRefresh" />
              <CacheCleanupPanel
                :vendor="vendor"
                :app="app"
                :source-epoch="sourceEpoch"
                :disabled="deleted"
              />
            </div>
          </template>
          <template #prewarm>
            <PrewarmPanel :vendor="vendor" :app="app" :read-only="deleted" />
          </template>
        </Tabs>
      </template>
    </div>
  </AsyncState>
</template>
