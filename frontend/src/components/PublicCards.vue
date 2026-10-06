<script setup lang="ts">
import ApplicationVersion from "./ApplicationVersion.vue";
import { applicationVendorName } from "../vendorName";
import { applicationCapabilities, type Application } from "../bootstrap";
import { language, t } from "../i18n";
import { useId } from "vue";
import Icon from "./Icon.vue";
const versionTooltipID = useId();
defineProps<{
  apps: (Application & { download_clients?: number })[];
  showAction?: boolean;
}>();
</script>
<template>
  <div class="application-list">
    <RouterLink
      v-for="app in apps"
      :key="app.id"
      :to="`/${app.id}`"
      class="application-card"
      :aria-describedby="app.latest_known_version?.first_seen ? `${versionTooltipID}-${app.id}` : undefined"
      ><div class="application-card-brand">
        <span class="application-card-icon"><img
          v-if="app.icon"
          :src="app.icon"
          alt=""
          width="40"
          height="40"
        /><Icon v-else name="box" :size="36" /></span>
        <div class="application-card-heading">
          <h2>{{ app.name[language] }}</h2>
          <div class="application-card-meta"><span class="app-publisher">{{ applicationVendorName(app) }}</span><ApplicationVersion :app="app" card :tooltip-id="`${versionTooltipID}-${app.id}`" /></div>
        </div>
      </div>
      <p>{{ app.summary[language] }}</p>
      <div v-if="showAction" class="application-card-footer">
      <span v-if="showAction" class="card-action">{{
        applicationCapabilities(app).installers
          ? t("Installation instructions")
          : app.provider === "info"
            ? t("Usage instructions")
            : t("Download files")
      }}<Icon name="arrow" /></span></div></RouterLink
    >
  </div>
</template>
