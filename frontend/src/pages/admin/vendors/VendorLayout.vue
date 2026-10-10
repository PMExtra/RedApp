<script setup lang="ts">
// Package B: vendor header (name, logo, export) and tab host.
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { RouterView, useRoute } from "vue-router";
import { NavTabs, PageHeader, type Crumb, type NavTabItem } from "@/shared/ui";

const { t } = useI18n();
const route = useRoute();
const vendor = computed(() => String(route.params.vendor));
const breadcrumbs = computed<Crumb[]>(() => [
  { label: t("adminShell.titles.vendors"), to: { name: "admin-vendors" } },
  { label: vendor.value },
]);
const tabs = computed<NavTabItem[]>(() => [
  { label: t("adminShell.tabs.vendorSettings"), to: { name: "admin-vendor-settings" } },
  { label: t("adminShell.tabs.vendorApps"), to: { name: "admin-vendor-apps" } },
  { label: t("adminShell.tabs.adminNotes"), to: { name: "admin-vendor-notes" } },
]);
</script>

<template>
  <div class="flex flex-col gap-6">
    <PageHeader :title="vendor" :breadcrumbs="breadcrumbs" />
    <NavTabs :items="tabs" :label="t('adminShell.tabs.label')" />
    <RouterView />
  </div>
</template>
