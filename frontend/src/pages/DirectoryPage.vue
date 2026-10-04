<script setup lang="ts">
import { computed, ref } from "vue";
import { directoryError, directoryLoading, loadDirectory, managedApps, vendors, applicationEnabled } from "../directory";
import { errorText, language, t } from "../i18n";
const filter = ref("all");
const visibleVendors = computed(() => vendors.value.filter((vendor) =>
  filter.value === "deleted" ? !!vendor.deleted_at || managedApps.value.some((app) => app.vendor_uid === vendor.uid && app.deleted_at) : !vendor.deleted_at && (filter.value !== "disabled" || !vendor.enabled || managedApps.value.some((app) => app.vendor_uid === vendor.uid && !app.deleted_at && !applicationEnabled(app))),
));
function appsFor(uid: string) {
  return managedApps.value.filter((app) => app.vendor_uid === uid &&
    (filter.value === "deleted" ? !!app.deleted_at : !app.deleted_at) &&
    (filter.value !== "disabled" || !applicationEnabled(app)));
}
</script>
<template>
  <section class="directory-page">
    <div class="page-heading">
      <div><h1>{{ t("Vendors and applications") }}</h1><p class="muted">{{ t("Manage application details, providers and availability.") }}</p></div>
      <RouterLink to="/admin/vendors/new" class="button-link">{{ t("Add vendor") }}</RouterLink>
    </div>
    <div class="directory-toolbar">
      <label>{{ t("Show") }}<select v-model="filter"><option value="all">{{ t("Current") }}</option><option value="disabled">{{ t("Disabled") }}</option><option value="deleted">{{ t("Deleted") }}</option></select></label>
      <button class="secondary" :disabled="directoryLoading" @click="loadDirectory">{{ t("Refresh") }}</button>
    </div>
    <p v-if="directoryError" class="error" role="alert">{{ errorText(directoryError) }}</p>
    <p v-if="directoryLoading" role="status">{{ t("Loading…") }}</p>
    <section v-for="vendor in visibleVendors" :key="vendor.uid" class="panel vendor-card">
      <div class="directory-heading">
        <img v-if="vendor.icon" :src="vendor.icon" alt="" width="40" height="40" />
        <div><h2><RouterLink :to="`/admin/vendors/${vendor.id}/settings`">{{ vendor.name[language] }}</RouterLink></h2><code>{{ vendor.id }}</code></div>
        <span v-if="vendor.deleted_at" class="state-label">{{ t("Deleted") }}</span><span v-else-if="!vendor.enabled" class="state-label">{{ t("Disabled") }}</span>
      </div>
      <p v-if="vendor.description[language]" class="muted">{{ vendor.description[language] }}</p>
      <ul class="directory-apps">
        <li v-for="app in appsFor(vendor.uid)" :key="app.uid">
          <RouterLink :to="`/admin/apps/${app.key}/settings`">{{ app.name[language] }}</RouterLink><code>{{ app.key }}</code><span>{{ app.provider }}</span>
          <span v-if="app.deleted_at" class="state-label">{{ t("Deleted") }}</span><span v-else-if="!applicationEnabled(app)" class="state-label">{{ t("Disabled") }}</span>
        </li>
      </ul>
      <RouterLink v-if="!vendor.deleted_at" :to="`/admin/vendors/${vendor.id}/apps/new`">{{ t("Add application") }}</RouterLink>
    </section>
    <p v-if="!directoryLoading && !directoryError && !visibleVendors.length" class="empty">{{ t("No vendors in this view.") }}</p>
  </section>
</template>
