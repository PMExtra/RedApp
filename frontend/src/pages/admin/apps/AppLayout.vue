<script setup lang="ts">
import { computed, ref } from "vue";
import { CopyPlus, Download, ExternalLink } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { RouterLink, RouterView, useRoute } from "vue-router";
import { useBootstrap } from "@/features/bootstrap";
import {
  appRoute,
  appTabFromRoute,
  appTabs,
  isPublished,
  useApp,
  useProviders,
  useVendor,
  type AppTab,
} from "@/features/directory";
import { CopyAppDialog, ExportDialog } from "@/features/exchange";
import { isApiError } from "@/shared/api";
import { useLocalized } from "@/shared/i18n";
import { publicUrl, useDocumentTitle } from "@/shared/lib";
import {
  Alert,
  AsyncState,
  Badge,
  Button,
  CopyButton,
  EmptyState,
  EntityIcon,
  NavTabs,
  PageHeader,
  type Crumb,
  type NavTabItem,
} from "@/shared/ui";

/**
 * Application header (icon, state, public link, download URL, export, copy)
 * and the tab host. Tabs follow the provider: files for `hosted`; cache for
 * `http-cache`, `codex` and `claude-code`; versions for `codex` and
 * `claude-code`. A tab the provider lacks shows "not available" instead.
 */
const { t } = useI18n();
const localized = useLocalized();
const route = useRoute();
const vendorId = computed(() => String(route.params.vendor));
const appId = computed(() => String(route.params.app));
const app = useApp(vendorId, appId);
const vendor = useVendor(vendorId);
const providers = useProviders();
const bootstrap = useBootstrap();
const exporting = ref(false);
const copying = ref(false);

const name = computed(() => {
  const data = app.data.value;
  return data ? localized(data.name) || data.id : appId.value;
});
useDocumentTitle(name);
const notFound = computed(() => isApiError(app.error.value, "APPLICATION_NOT_FOUND"));
const vendorName = computed(() => {
  const data = vendor.data.value;
  return data ? localized(data.name) || data.id : vendorId.value;
});
const providerName = computed(() => {
  const key = app.data.value?.provider;
  const item = providers.data.value?.items.find((entry) => entry.key === key);
  return item ? localized(item.name) : (key ?? "");
});
const published = computed(() =>
  app.data.value ? isPublished(app.data.value, vendor.data.value) : false,
);
const publicPath = computed(() => `/${vendorId.value}/${appId.value}`);
const downloadUrl = computed(() =>
  publicUrl(bootstrap.data.value?.public_url ?? window.location.origin, `${publicPath.value}/`),
);

const breadcrumbs = computed<Crumb[]>(() => [
  { label: t("adminShell.titles.vendors"), to: { name: "admin-vendors" } },
  {
    label: vendorName.value,
    to: { name: "admin-vendor-apps", params: { vendor: vendorId.value } },
  },
  { label: name.value },
]);

const TAB_LABELS: Record<AppTab, string> = {
  settings: "adminShell.tabs.appSettings",
  versions: "adminShell.tabs.versions",
  cache: "adminShell.tabs.cache",
  files: "adminShell.tabs.files",
  "admin-notes": "adminShell.tabs.adminNotes",
};
const available = computed<AppTab[]>(() => (app.data.value ? appTabs(app.data.value) : []));
const tabs = computed<NavTabItem[]>(() => {
  const data = app.data.value;
  if (!data) return [];
  return available.value.map((tab) => ({ label: t(TAB_LABELS[tab]), to: appRoute(data, tab) }));
});
const currentTab = computed(() => appTabFromRoute(route.name));
const tabAvailable = computed(
  () => currentTab.value !== undefined && available.value.includes(currentTab.value),
);
</script>

<template>
  <div class="flex flex-col gap-6">
    <template v-if="notFound">
      <PageHeader :title="`${vendorId}/${appId}`" :breadcrumbs="breadcrumbs" />
      <EmptyState
        :title="t('directory.app.notFound')"
        :description="t('directory.app.notFoundHint')"
      >
        <template #action>
          <Button as-child>
            <RouterLink :to="{ name: 'admin-vendor-apps', params: { vendor: vendorId } }">
              {{ t("directory.app.backToVendor") }}
            </RouterLink>
          </Button>
        </template>
      </EmptyState>
    </template>
    <AsyncState
      v-else
      :loading="app.isPending.value"
      :error="app.data.value ? undefined : app.error.value"
      @retry="app.refetch()"
    >
      <div v-if="app.data.value" class="flex flex-col gap-6">
        <PageHeader :title="name" :breadcrumbs="breadcrumbs">
          <template #media>
            <EntityIcon :src="app.data.value.icon" size="xl" />
          </template>
          <template #meta>
            <span class="font-mono">{{ app.data.value.key }}</span>
            <span>{{ providerName }}</span>
            <Badge v-if="app.data.value.deleted_at" tone="danger">
              {{ t("directory.state.deletedOne") }}
            </Badge>
            <Badge v-else-if="!app.data.value.enabled" tone="warning">
              {{ t("directory.state.disabledOne") }}
            </Badge>
            <Badge v-else-if="published" tone="success">{{ t("directory.state.published") }}</Badge>
            <Badge v-else-if="vendor.data.value" tone="warning">
              {{ t("directory.state.disabledByVendor") }}
            </Badge>
            <Badge v-if="app.data.value.builtin_template">{{ t("directory.builtin") }}</Badge>
          </template>
          <template #actions>
            <template v-if="published">
              <Button as-child>
                <a :href="publicPath" target="_blank" rel="noopener">
                  <ExternalLink aria-hidden="true" /> {{ t("directory.app.publicPage") }}
                </a>
              </Button>
              <CopyButton
                v-if="app.data.value.provider !== 'info'"
                :text="downloadUrl"
                :label="t('directory.app.copyUrl')"
                show-label
                size="md"
              />
            </template>
            <template v-if="!app.data.value.deleted_at">
              <Button @click="exporting = true">
                <Download aria-hidden="true" /> {{ t("exchange.export.action") }}
              </Button>
              <Button @click="copying = true">
                <CopyPlus aria-hidden="true" /> {{ t("exchange.copy.action") }}
              </Button>
            </template>
          </template>
        </PageHeader>
        <Alert v-if="app.data.value.deleted_at" tone="warning">
          {{ t("directory.readOnly.app") }}
        </Alert>
        <NavTabs :items="tabs" :label="t('adminShell.tabs.label')" />
        <RouterView v-if="tabAvailable" />
        <EmptyState
          v-else
          :title="t('directory.app.tabUnavailable')"
          :description="t('directory.app.tabUnavailableHint', { provider: providerName })"
        >
          <template #action>
            <Button as-child>
              <RouterLink :to="appRoute(app.data.value, 'settings')">
                {{ t("directory.app.openSettings") }}
              </RouterLink>
            </Button>
          </template>
        </EmptyState>
        <ExportDialog v-model:open="exporting" kind="app" :entity-key="app.data.value.key" />
        <CopyAppDialog v-if="copying" v-model:open="copying" :app="app.data.value" />
      </div>
    </AsyncState>
  </div>
</template>
