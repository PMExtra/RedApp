<script setup lang="ts">
import AutoRefresh from "../components/AutoRefresh.vue";
import { ref } from "vue";
import { type Metric } from "../api";
import { usePageStatus } from "../composables/usePageStatus";
import { errorText, localDate, t } from "../i18n";
import Overview from "../components/Overview.vue";
import HistoryDialog from "../components/HistoryDialog.vue";
import Icon from "../components/Icon.vue";
const { status, error, loading, automatic, refresh } = usePageStatus(
  ref("status"),
);
const activeMetric = ref<Metric>();
</script>
<template>
  <div class="page-heading">
    <h1>
      {{ t("Distribution overview") }}
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
      <AutoRefresh v-model="automatic" /><button
        class="secondary"
        :disabled="loading"
        @click="refresh"
      >
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
  <Overview v-else :status="status" @history="activeMetric = $event" />
  <HistoryDialog
    v-if="activeMetric"
    :metric="activeMetric"
    @close="activeMetric = undefined"
    @error="error = $event"
  />
</template>
