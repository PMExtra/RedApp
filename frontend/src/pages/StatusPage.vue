<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { appAPI } from "../bootstrap";
import { type Metric } from "../api";
import { usePageStatus } from "../composables/usePageStatus";
import { errorText, localDate, t } from "../i18n";
import Overview from "../components/Overview.vue";
import Resources from "../components/Resources.vue";
import HistoryDialog from "../components/HistoryDialog.vue";
import Icon from "../components/Icon.vue";
const props = defineProps<{ kind: "overview" | "versions" }>();
const route = useRoute();
const application = computed(
  () => `${route.params.vendor}/${route.params.app}`,
);
const path = computed(() =>
  props.kind === "versions" ? `${appAPI(application.value)}/status` : "status",
);
const { status, error, loading, automatic, refresh } = usePageStatus(path);
const activeMetric = ref<Metric>();
watch(path, () => {
  activeMetric.value = undefined;
});
</script>
<template>
  <div class="page-heading">
    <h1>
      {{ kind === "overview" ? t("Distribution overview") : t("Versions") }}
    </h1>
  </div>
  <div class="snapshot-toolbar">
    <span class="snapshot"
      ><Icon name="clock" :size="16" />{{ t("Snapshot")
      }}<time
        v-if="status"
        :datetime="status.sampled_at"
        :title="t('Local time')"
        >{{ localDate(status.sampled_at) }}</time
      ><span v-else>—</span></span
    >
    <div class="refresh-actions">
      <button
        class="secondary auto-refresh"
        :aria-pressed="automatic"
        :title="t('Refresh every 5 seconds')"
        @click="automatic = !automatic"
      >
        <Icon name="refresh" />{{ t("Auto refresh") }}
        <span>{{ automatic ? t("On") : t("Off") }}</span></button
      ><button class="secondary" :disabled="loading" @click="refresh">
        {{ loading ? t("Refreshing…") : t("Refresh") }}
      </button>
    </div>
  </div>
  <div v-if="error" class="error" role="alert">
    <div>
      {{ errorText(error)
      }}<small v-if="status">{{
        t("Showing the last successful snapshot.")
      }}</small>
    </div>
    <button class="secondary" :disabled="loading" @click="refresh">
      {{ t("Retry") }}
    </button>
  </div>
  <div v-if="!status" class="empty panel" role="status">
    {{
      loading
        ? t("Loading service status…")
        : t("Status unavailable. Retry to reconnect.")
    }}
  </div>
  <Overview
    v-else
    :status="status"
    :application="kind === 'versions' ? application : undefined"
    @history="activeMetric = $event"
  />
  <Resources
    v-if="kind === 'versions'"
    :application="application"
    :automatic="automatic"
  />
  <HistoryDialog
    v-if="activeMetric"
    :metric="activeMetric"
    :application="kind === 'versions' ? application : undefined"
    @close="activeMetric = undefined"
    @error="error = $event"
  />
</template>
