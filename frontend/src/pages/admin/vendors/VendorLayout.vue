<script setup lang="ts">
import { computed, ref } from "vue";
import { Download } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { RouterLink, RouterView, useRoute } from "vue-router";
import { useVendor, vendorLogo } from "@/features/directory";
import { ExportDialog } from "@/features/exchange";
import { isApiError } from "@/shared/api";
import { useLocalized } from "@/shared/i18n";
import { useDocumentTitle } from "@/shared/lib";
import {
  Alert,
  AsyncState,
  Badge,
  Button,
  EmptyState,
  EntityIcon,
  NavTabs,
  PageHeader,
  type Crumb,
  type NavTabItem,
} from "@/shared/ui";

/** Vendor header (name, logo, state, export) and the vendor tabs. */
const { t, locale } = useI18n();
const localized = useLocalized();
const route = useRoute();
const vendorId = computed(() => String(route.params.vendor));
const vendor = useVendor(vendorId);
const exporting = ref(false);

const name = computed(() => {
  const data = vendor.data.value;
  return data ? localized(data.name) || data.id : vendorId.value;
});
useDocumentTitle(name);
const notFound = computed(() => isApiError(vendor.error.value, "VENDOR_NOT_FOUND"));

const breadcrumbs = computed<Crumb[]>(() => [
  { label: t("adminShell.titles.vendors"), to: { name: "admin-vendors" } },
  { label: name.value },
]);
const tabs = computed<NavTabItem[]>(() => [
  { label: t("adminShell.tabs.vendorSettings"), to: { name: "admin-vendor-settings" } },
  { label: t("adminShell.tabs.vendorApps"), to: { name: "admin-vendor-apps" } },
  { label: t("adminShell.tabs.adminNotes"), to: { name: "admin-vendor-notes" } },
]);
</script>

<template>
  <div class="flex flex-col gap-6">
    <template v-if="notFound">
      <PageHeader :title="vendorId" :breadcrumbs="breadcrumbs" />
      <EmptyState
        :title="t('directory.vendor.notFound')"
        :description="t('directory.vendor.notFoundHint')"
      >
        <template #action>
          <Button as-child>
            <RouterLink :to="{ name: 'admin-vendors' }">{{ t("directory.backToList") }}</RouterLink>
          </Button>
        </template>
      </EmptyState>
    </template>
    <AsyncState
      v-else
      :loading="vendor.isPending.value"
      :error="vendor.data.value ? undefined : vendor.error.value"
      @retry="vendor.refetch()"
    >
      <template v-if="vendor.data.value">
        <div class="flex flex-col gap-6">
          <PageHeader :title="name" :breadcrumbs="breadcrumbs">
            <template #meta>
              <span class="font-mono">{{ vendor.data.value.id }}</span>
              <Badge v-if="vendor.data.value.deleted_at" tone="danger">
                {{ t("directory.state.deletedOne") }}
              </Badge>
              <Badge v-else-if="!vendor.data.value.enabled" tone="warning">
                {{ t("directory.state.disabledOne") }}
              </Badge>
              <Badge v-else tone="success">{{ t("directory.state.enabledOne") }}</Badge>
              <Badge v-if="vendor.data.value.has_template">{{ t("directory.builtin") }}</Badge>
            </template>
            <template #actions>
              <EntityIcon :src="vendorLogo(vendor.data.value, locale)" variant="logo" size="lg" />
              <Button v-if="!vendor.data.value.deleted_at" @click="exporting = true">
                <Download aria-hidden="true" /> {{ t("exchange.export.action") }}
              </Button>
            </template>
          </PageHeader>
          <Alert v-if="vendor.data.value.deleted_at" tone="warning">
            {{ t("directory.readOnly.vendor") }}
          </Alert>
          <NavTabs :items="tabs" :label="t('adminShell.tabs.label')" />
          <RouterView />
          <ExportDialog v-model:open="exporting" kind="vendor" :entity-key="vendor.data.value.id" />
        </div>
      </template>
    </AsyncState>
  </div>
</template>
