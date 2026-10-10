<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  PathMatchInput,
  patternTooLong,
  type CleanupBasis,
  type PathMatch,
} from "@/features/cache-policy";
import { Alert, Button, Card, Field, Input, RadioGroup, type RadioOption } from "@/shared/ui";
import MaintenanceReview from "./MaintenanceReview.vue";
import { localTimeZone, localToUtc } from "./cutoff";
import { useMaintenanceActions } from "./queries";

/**
 * Removes cached files matching a pattern whose download or last access is
 * older than a cutoff, in the selected source epoch.
 */
const props = defineProps<{
  vendor: string;
  app: string;
  /** `null` for the current source epoch. */
  sourceEpoch: number | null;
  disabled?: boolean;
}>();
/** `busy`: a preview or job is open, so the page must keep the source epoch. */
const emit = defineEmits<{ "update:busy": [busy: boolean] }>();
const { t } = useI18n();
const timeZone = localTimeZone();
const match = ref<PathMatch>({ type: "glob", pattern: "/" });
const basis = ref<CleanupBasis>("last_access");
const beforeLocal = ref("");
const previewId = ref<string | null>(null);
const stale = ref(false);
const jobActive = ref(false);
const submitted = ref(false);
const { create } = useMaintenanceActions(
  "cleanup",
  () => props.vendor,
  () => props.app,
);

const before = computed(() => localToUtc(beforeLocal.value));
const beforeError = computed(() => {
  if (!submitted.value && !beforeLocal.value) return undefined;
  return before.value ? undefined : t("httpCache.cleanup.beforeInvalid");
});
const basisOptions = computed<RadioOption[]>(() => [
  {
    value: "last_access",
    label: t("cachePolicy.basis.last_access"),
    description: t("httpCache.cleanup.lastAccessHint"),
  },
  {
    value: "fetched_at",
    label: t("cachePolicy.basis.fetched_at"),
    description: t("cachePolicy.basis.fetchedAtHint"),
  },
]);
const basisValue = computed({
  get: () => basis.value,
  set: (value: string | undefined) => {
    if (value === "last_access" || value === "fetched_at") basis.value = value;
  },
});
const locked = computed(() => props.disabled || create.isPending.value || jobActive.value);

// A preview belongs to the selection it was made from.
watch([match, basis, beforeLocal, () => props.sourceEpoch], () => close(false), { deep: true });

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
  submitted.value = true;
  const cutoff = before.value;
  if (!cutoff || !match.value.pattern || patternTooLong(match.value.pattern) || props.disabled) {
    return;
  }
  stale.value = false;
  create.mutate(
    {
      match: { ...match.value },
      basis: basis.value,
      before: cutoff,
      ...(props.sourceEpoch === null ? {} : { source_epoch: props.sourceEpoch }),
    },
    {
      onSuccess: (preview) => {
        previewId.value = preview.id;
      },
    },
  );
}
</script>

<template>
  <Card :title="t('httpCache.cleanup.title')" :description="t('httpCache.cleanup.description')">
    <div class="flex flex-col gap-4">
      <form class="flex flex-col gap-4" novalidate @submit.prevent="submit">
        <PathMatchInput v-model="match" :legend="t('httpCache.cleanup.title')" :disabled="locked" />
        <fieldset class="flex flex-col gap-2" :disabled="locked">
          <legend class="mb-1 text-sm font-medium">{{ t("cachePolicy.basis.label") }}</legend>
          <RadioGroup v-model="basisValue" :options="basisOptions" :disabled="locked" />
        </fieldset>
        <Field
          v-slot="{ control }"
          class="w-72"
          :label="t('httpCache.cleanup.before', { zone: timeZone })"
          :description="
            before
              ? t('httpCache.cleanup.utc', { time: before })
              : t('httpCache.cleanup.beforeHint')
          "
          :error="beforeError"
          required
        >
          <Input
            v-bind="control"
            v-model="beforeLocal"
            type="datetime-local"
            step="60"
            :disabled="locked"
          />
        </Field>
        <div>
          <Button type="submit" :loading="create.isPending.value" :disabled="locked">
            {{ t("httpCache.cleanup.preview") }}
          </Button>
        </div>
      </form>
      <Alert v-if="stale" tone="warning" :title="t('releases.preview.staleTitle')">
        {{ t("releases.preview.staleDescription") }}
      </Alert>
      <MaintenanceReview
        v-if="previewId"
        :key="previewId"
        kind="cleanup"
        :vendor="vendor"
        :app="app"
        :preview-id="previewId"
        :disabled="disabled"
        @update:active="jobActive = $event"
        @discard="close(false)"
        @stale="close(true)"
      />
    </div>
  </Card>
</template>
