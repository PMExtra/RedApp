<script setup lang="ts">
import { applicationVendorName } from "../vendorName";
import type { Application } from "../bootstrap";
import { language, t } from "../i18n";
import Icon from "./Icon.vue";
defineProps<{ apps: (Application & { download_clients?: number })[] }>();
</script>
<template>
  <div class="application-list">
    <RouterLink
      v-for="app in apps"
      :key="app.id"
      :to="`/${app.id}`"
      class="application-card"
      ><div class="application-card-brand">
        <img
          v-if="app.icon"
          :src="app.icon"
          alt=""
          width="40"
          height="40"
        /><Icon v-else name="box" :size="40" /><span class="app-publisher">{{
          applicationVendorName(app)
        }}</span>
      </div>
      <h2>{{ app.name[language] }}</h2>
      <p>{{ app.summary[language] }}</p>
      <span v-if="app.download_clients !== undefined" class="ranking-count">{{
        t("About {count} download clients", { count: app.download_clients })
      }}</span></RouterLink
    >
  </div>
</template>
