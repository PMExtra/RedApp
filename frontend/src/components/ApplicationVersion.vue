<script setup lang="ts">
import { computed } from "vue";
import { applicationCapabilities, type Application } from "../bootstrap";
import { language, t } from "../i18n";
import RelativeTime from "./RelativeTime.vue";
const props = defineProps<{ app: Application; card?: boolean; tooltipId?: string }>();
const discoveryDate = computed(() => {
  const value = props.app.latest_known_version?.first_seen;
  const date = value ? new Date(value) : undefined;
  return date && Number.isFinite(date.getTime()) && date.getTime() > 0
    ? new Intl.DateTimeFormat(language.value, { year: "numeric", month: "short", day: "numeric" }).format(date)
    : undefined;
});
</script>
<template>
  <template v-if="applicationCapabilities(app).versions">
    <span v-if="card && app.latest_known_version" class="card-version relative-time" :title="discoveryDate">
      <strong>{{ app.latest_known_version.version }}</strong>
      <span v-if="discoveryDate" :id="tooltipId" role="tooltip" class="relative-time-tooltip">{{ discoveryDate }}</span>
    </span>
    <div v-else-if="!card" class="application-version">
      <template v-if="app.latest_known_version">
        <strong>{{ app.latest_known_version.version }}</strong>
        <RelativeTime :value="app.latest_known_version.first_seen" />
      </template>
      <span v-else>{{ t("Version not known yet") }}</span>
    </div>
  </template>
</template>
