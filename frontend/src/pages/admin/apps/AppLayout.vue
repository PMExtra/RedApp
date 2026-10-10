<script setup lang="ts">
// Package B: application header (icon, export, copy, public link) and tab host.
// Tabs must follow provider capabilities (see x-spa-routes): files for hosted;
// cache for http-cache, codex, claude-code; versions for codex, claude-code.
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { RouterView, useRoute } from "vue-router";
import { NavTabs, PageHeader, type Crumb, type NavTabItem } from "@/shared/ui";

const { t } = useI18n();
const route = useRoute();
const vendor = computed(() => String(route.params.vendor));
const app = computed(() => String(route.params.app));
const breadcrumbs = computed<Crumb[]>(() => [
  { label: t("adminShell.titles.vendors"), to: { name: "admin-vendors" } },
  { label: vendor.value, to: { name: "admin-vendor-apps", params: { vendor: vendor.value } } },
  { label: app.value },
]);
const tabs = computed<NavTabItem[]>(() => [
  { label: t("adminShell.tabs.appSettings"), to: { name: "admin-app-settings" } },
  { label: t("adminShell.tabs.versions"), to: { name: "admin-app-versions" } },
  { label: t("adminShell.tabs.cache"), to: { name: "admin-app-cache" } },
  { label: t("adminShell.tabs.files"), to: { name: "admin-app-files" } },
  { label: t("adminShell.tabs.adminNotes"), to: { name: "admin-app-notes" } },
]);
</script>

<template>
  <div class="flex flex-col gap-6">
    <PageHeader :title="`${vendor}/${app}`" :breadcrumbs="breadcrumbs" />
    <NavTabs :items="tabs" :label="t('adminShell.tabs.label')" />
    <RouterView />
  </div>
</template>
