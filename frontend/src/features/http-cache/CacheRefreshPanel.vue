<script setup lang="ts">
import { ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { PathMatchInput, patternTooLong, type PathMatch } from "@/features/cache-policy";
import { Alert, Button, Card } from "@/shared/ui";
import MaintenanceReview from "./MaintenanceReview.vue";
import { useMaintenanceActions } from "./queries";

/**
 * Revalidates every cached file matching a pattern: a frozen preview, then a
 * background run. Only the current, active source can be refreshed.
 */
const props = defineProps<{ vendor: string; app: string; available: boolean }>();
/** `busy`: a preview or job is open, so the page must keep the source epoch. */
const emit = defineEmits<{ "update:busy": [busy: boolean] }>();
const { t } = useI18n();
const match = ref<PathMatch>({ type: "glob", pattern: "/" });
const previewId = ref<string | null>(null);
const stale = ref(false);
const jobActive = ref(false);
const { create } = useMaintenanceActions(
  "refresh",
  () => props.vendor,
  () => props.app,
);

watch(
  match,
  () => {
    previewId.value = null;
  },
  { deep: true },
);

watch(
  () => previewId.value !== null || jobActive.value || create.isPending.value,
  (busy) => {
    emit("update:busy", busy);
  },
  { immediate: true },
);

function close(expired: boolean) {
  previewId.value = null;
  jobActive.value = false;
  stale.value = expired;
}

function submit() {
  if (!match.value.pattern || patternTooLong(match.value.pattern) || !props.available) return;
  stale.value = false;
  create.mutate(
    { match: { ...match.value } },
    {
      onSuccess: (preview) => {
        previewId.value = preview.id;
      },
    },
  );
}
</script>

<template>
  <Card :title="t('httpCache.refresh.title')" :description="t('httpCache.refresh.description')">
    <div class="flex flex-col gap-4">
      <Alert v-if="!available" tone="info">{{ t("httpCache.refresh.unavailable") }}</Alert>
      <form class="flex flex-col gap-3" novalidate @submit.prevent="submit">
        <PathMatchInput
          v-model="match"
          :legend="t('httpCache.refresh.title')"
          :disabled="!available || create.isPending.value || jobActive"
        />
        <div>
          <Button
            type="submit"
            :loading="create.isPending.value"
            :disabled="!available || jobActive || !match.pattern || patternTooLong(match.pattern)"
          >
            {{ t("httpCache.refresh.preview") }}
          </Button>
        </div>
      </form>
      <Alert v-if="stale" tone="warning" :title="t('releases.preview.staleTitle')">
        {{ t("releases.preview.staleDescription") }}
      </Alert>
      <MaintenanceReview
        v-if="previewId"
        :key="previewId"
        kind="refresh"
        :vendor="vendor"
        :app="app"
        :preview-id="previewId"
        :disabled="!available"
        @update:active="jobActive = $event"
        @discard="close(false)"
        @stale="close(true)"
      />
    </div>
  </Card>
</template>
