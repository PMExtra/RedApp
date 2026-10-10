<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { isApiError } from "@/shared/api";
import { randomId } from "@/shared/lib";
import { AsyncState, Card } from "@/shared/ui";
import AutoPrewarmPolicy from "./AutoPrewarmPolicy.vue";
import PrewarmJobView from "./PrewarmJobView.vue";
import PrewarmStartForm from "./PrewarmStartForm.vue";
import {
  loadJobId,
  storeJobId,
  usePrewarmActions,
  usePrewarmJob,
  usePrewarmOptions,
  type PrewarmJob,
} from "./queries";

/**
 * Prewarming of a release or HTTP cache application: start a one-time task,
 * follow it (the job ID survives reloads), cancel or retry it, and for release
 * applications the automatic prewarm policy. `inactive` (disabled application
 * or vendor) keeps the policy editable but blocks starting and retrying jobs.
 */
const props = defineProps<{
  vendor: string;
  app: string;
  readOnly?: boolean;
  inactive?: boolean;
}>();
const { t } = useI18n();
const options = usePrewarmOptions(
  () => props.vendor,
  () => props.app,
);
const jobId = ref<string | null>(loadJobId(props.vendor, props.app));
watch([() => props.vendor, () => props.app], ([vendor, app]) => {
  jobId.value = loadJobId(vendor, app);
});
const job = usePrewarmJob(
  () => props.vendor,
  () => props.app,
  jobId,
);
const actions = usePrewarmActions(
  () => props.vendor,
  () => props.app,
);
const current = computed(() => (jobId.value ? job.data.value : undefined));
const running = computed(() => current.value?.state === "running");
const busy = computed(() => actions.cancel.isPending.value || actions.retry.isPending.value);

function show(next: PrewarmJob | null) {
  // A new job answers an earlier "another task is running" retry error.
  if (next) actions.retry.reset();
  jobId.value = next?.id ?? null;
  storeJobId(props.vendor, props.app, jobId.value);
}

// A job is kept for 24 hours; forget one the server no longer knows.
watch(
  () => job.error.value,
  (error) => {
    if (isApiError(error, "JOB_NOT_FOUND")) show(null);
  },
);

function cancel() {
  if (!current.value) return;
  actions.cancel.mutate(current.value.id, {
    onError: (error) => {
      if (isApiError(error, "JOB_NOT_FOUND")) show(null);
    },
  });
}

function retry() {
  if (!current.value) return;
  actions.retry.mutate(
    { id: current.value.id, requestId: randomId() },
    {
      onSuccess: show,
      onError: (error) => {
        if (isApiError(error, "JOB_NOT_FOUND")) show(null);
      },
    },
  );
}
</script>

<template>
  <Card :title="t('prewarm.title')" :description="t('prewarm.description')">
    <AsyncState
      :loading="options.isPending.value"
      :error="options.error.value"
      @retry="options.refetch()"
    >
      <div v-if="options.data.value" class="flex flex-col gap-6">
        <PrewarmStartForm
          v-if="!readOnly"
          :key="`${vendor}/${app}`"
          :vendor="vendor"
          :app="app"
          :options="options.data.value"
          :disabled="running || inactive"
          @started="show"
        />
        <p
          v-if="isApiError(actions.retry.error.value, 'PREWARM_BUSY')"
          class="text-sm text-warning"
          role="alert"
        >
          {{ t("prewarm.start.busy") }}
        </p>
        <PrewarmJobView
          v-if="current"
          :vendor="vendor"
          :app="app"
          :job="current"
          :busy="busy"
          :read-only="readOnly"
          :inactive="inactive"
          @cancel="cancel"
          @retry="retry"
          @dismiss="show(null)"
        />
        <section
          v-if="options.data.value.kind === 'release'"
          class="flex flex-col gap-3 border-t border-border pt-6"
          aria-labelledby="prewarm-auto-title"
        >
          <h3 id="prewarm-auto-title" class="text-sm font-semibold">
            {{ t("prewarm.auto.title") }}
          </h3>
          <AutoPrewarmPolicy
            :vendor="vendor"
            :app="app"
            :options="options.data.value"
            :read-only="readOnly"
          />
        </section>
      </div>
    </AsyncState>
  </Card>
</template>
