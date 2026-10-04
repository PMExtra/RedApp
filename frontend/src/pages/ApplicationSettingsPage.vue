<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";
import Maintenance from "../components/Maintenance.vue";
import DirectoryEditor from "../components/DirectoryEditor.vue";
import GeneralCachePolicy from "../components/GeneralCachePolicy.vue";
import { managedApps, providerHasVersions, providerHasTimeCleanup } from "../directory";
import { t } from "../i18n";
const route = useRoute();
const application = computed(
  () => `${route.params.vendor}/${route.params.app}`,
);
const record = computed(() => managedApps.value.find((item) => item.key === application.value));
</script>
<template><DirectoryEditor kind="app" /><p v-if="record && providerHasTimeCleanup(record.provider)"><RouterLink :to="`/admin/apps/${application}/cache`">{{ t('Cached files') }}</RouterLink></p><GeneralCachePolicy v-if="record && !record.deleted_at && providerHasTimeCleanup(record.provider)" :application="application" /><Maintenance v-if="record && providerHasVersions(record.provider)" :application="application" :show-settings="!record.deleted_at" /></template>
