<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";
import DirectoryApps from "../components/DirectoryApps.vue";
import { applicationNavigation, type Vendor } from "../directory";
import { language, t } from "../i18n";
defineProps<{ vendor: Vendor }>();
const route = useRoute();
const query = computed(() => typeof route.query.q === "string" ? route.query.q : "");
const state = computed(() => ["enabled", "disabled"].includes(String(route.query.state)) ? String(route.query.state) : "current");
</script>
<template>
  <section class="panel vendor-applications">
    <h2>{{ applicationNavigation[language] }}</h2>
    <p v-if="query || state !== 'current'" class="notice">
      {{ t('Filtered applications') }}<template v-if="query">: {{ query }}</template>
      <template v-if="state !== 'current'"> · {{ state === 'enabled' ? t('Enabled') : t('Disabled') }}</template>
      · <RouterLink :to="`/admin/vendors/${vendor.id}/apps`">{{ t('Show unfiltered applications') }}</RouterLink>
    </p>
    <DirectoryApps :vendor="vendor" :query="query" :state="state" />
  </section>
</template>
