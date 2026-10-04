<script setup lang="ts">
import AutoRefresh from "../components/AutoRefresh.vue";
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { appAPI } from "../bootstrap";
import type { Metric, Status } from "../api";
import { usePageStatus } from "../composables/usePageStatus";
import { errorText, localDate, t } from "../i18n";
import Overview from "../components/Overview.vue";
import Resources from "../components/Resources.vue";
import HistoryDialog from "../components/HistoryDialog.vue";
const route = useRoute();
const application = computed(
  () => `${route.params.vendor}/${route.params.app}`,
);
const path = computed(() => `${appAPI(application.value)}/status`);
const { status, error, loading, automatic, refresh } = usePageStatus(path);
const activeMetric = ref<Metric>();
const emptyStatus = { metrics: [] } as unknown as Status;
watch(path, () => (activeMetric.value = undefined));
</script>
<template>
  <div class="page-stack application-resources">
    <div class="snapshot-toolbar">
      <span class="muted"
        >{{ t("Snapshot") }}
        <time v-if="status" :datetime="status.sampled_at">{{
          localDate(status.sampled_at)
        }}</time></span
      >
      <div class="refresh-actions">
        <AutoRefresh v-model="automatic" /><button
          class="secondary"
          :disabled="loading"
          @click="refresh"
        >
          {{ t("Refresh") }}
        </button>
      </div>
    </div>
    <p v-if="error" class="error" role="alert">
      {{ errorText(error)
      }}<small v-if="status">{{
        t("Showing the last successful snapshot.")
      }}</small
      ><button class="secondary" @click="refresh">{{ t("Retry") }}</button>
    </p>
    <Overview
      :status="status || emptyStatus"
      :application="application"
      compact
      @history="activeMetric = $event"
      ><template #content
        ><Resources
          :application="application"
          :automatic="automatic" /></template
    ></Overview>
    <HistoryDialog
      v-if="activeMetric"
      :metric="activeMetric"
      :application="application"
      @close="activeMetric = undefined"
      @error="error = $event"
    />
  </div>
</template>
