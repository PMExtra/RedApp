<script setup lang="ts">
// Cache tab: prewarm, retention and version cleanup of release applications;
// cached files, refresh, cleanup and prewarm of HTTP cache applications.
// AppLayout only shows it for those providers and marks deleted applications.
// Prewarm, retention runs and cache refresh need an enabled application and
// vendor (otherwise APPLICATION_DISABLED); the page says so up front.
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRoute } from "vue-router";
import {
  AutoCleanupStatus,
  CacheCleanupPanel,
  CacheEntriesPanel,
  CacheRefreshPanel,
} from "@/features/http-cache";
import { appRoute, hasVersions, useApp, useVendor, vendorRoute } from "@/features/directory";
import { PrewarmPanel } from "@/features/prewarm";
import { SourceEpochSelect, VersionCleanup, useSources } from "@/features/releases";
import { RetentionPanel } from "@/features/retention";
import { Alert, AsyncState, Button, Tabs, type TabItem } from "@/shared/ui";

const { t } = useI18n();
const route = useRoute();
const vendor = computed(() => String(route.params.vendor));
const app = computed(() => String(route.params.app));
const record = useApp(vendor, app);
const provider = computed(() => record.data.value?.provider);
const vendorRecord = useVendor(vendor);
const deleted = computed(() => Boolean(record.data.value?.deleted_at));
// Which switch has to be turned on before the runtime actions work.
const disabledBy = computed<"app" | "vendor" | null>(() => {
  const data = record.data.value;
  if (!data || data.deleted_at) return null;
  if (!data.enabled) return "app";
  return vendorRecord.data.value?.enabled === false ? "vendor" : null;
});
const inactive = computed(() => disabledBy.value !== null);

// HTTP cache: one source epoch selection for the file list and cleanup.
const sourceEpoch = ref<number | null>(null);
watch([vendor, app], () => {
  sourceEpoch.value = null;
});
const sources = useSources(vendor, app);
const canRefresh = computed(() => {
  const current = sources.data.value?.items.find((source) => source.current);
  return (
    sourceEpoch.value === null && !deleted.value && !inactive.value && current?.active === true
  );
});
// An open refresh or cleanup preview belongs to the selected epoch: keep it
// until the preview is discarded or the job finishes.
const refreshBusy = ref(false);
const cleanupBusy = ref(false);
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
    <div v-if="provider && record.data.value" class="flex flex-col gap-6">
      <Alert v-if="disabledBy" tone="warning" :title="t('directory.inactive.title')">
        {{ t(`directory.inactive.${disabledBy}`) }}
        <template #actions>
          <Button size="sm" as-child>
            <RouterLink
              :to="
                disabledBy === 'app'
                  ? appRoute(record.data.value, 'settings')
                  : vendorRoute(record.data.value.vendor_id)
              "
            >
              {{
                disabledBy === "app"
                  ? t("directory.inactive.openApp")
                  : t("directory.inactive.openVendor")
              }}
            </RouterLink>
          </Button>
        </template>
      </Alert>
      <template v-if="hasVersions(provider)">
        <PrewarmPanel :vendor="vendor" :app="app" :read-only="deleted" :inactive="inactive" />
        <RetentionPanel :vendor="vendor" :app="app" :read-only="deleted" :inactive="inactive" />
        <VersionCleanup :vendor="vendor" :app="app" :disabled="deleted" />
      </template>

      <template v-else>
        <SourceEpochSelect
          v-model="sourceEpoch"
          class="max-w-2xl"
          :vendor="vendor"
          :app="app"
          :disabled="refreshBusy || cleanupBusy"
        />
        <!-- Kept mounted: previews and running jobs survive switching tabs. -->
        <Tabs
          v-model="section"
          :items="sections"
          :label="t('httpCache.sections.label')"
          keep-mounted
        >
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
              <CacheRefreshPanel
                :vendor="vendor"
                :app="app"
                :available="canRefresh"
                @update:busy="refreshBusy = $event"
              />
              <CacheCleanupPanel
                :vendor="vendor"
                :app="app"
                :source-epoch="sourceEpoch"
                :disabled="deleted"
                @update:busy="cleanupBusy = $event"
              />
            </div>
          </template>
          <template #prewarm>
            <PrewarmPanel :vendor="vendor" :app="app" :read-only="deleted" :inactive="inactive" />
          </template>
        </Tabs>
      </template>
    </div>
  </AsyncState>
</template>
