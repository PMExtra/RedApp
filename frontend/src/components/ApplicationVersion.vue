<script setup lang="ts">
import { applicationCapabilities, type Application } from "../bootstrap";
import { t } from "../i18n";
import RelativeTime from "./RelativeTime.vue";
defineProps<{ app: Application }>();
</script>
<template>
  <div v-if="applicationCapabilities(app).versions" class="application-version">
    <template v-if="app.latest_known_version">
      <span>{{ t("Latest known version") }} <strong>{{ app.latest_known_version.version }}</strong></span>
      <span>{{ t("Discovered") }} <RelativeTime :value="app.latest_known_version.first_seen" /></span>
    </template>
    <span v-else>{{ t("Version not known yet") }}</span>
  </div>
</template>
