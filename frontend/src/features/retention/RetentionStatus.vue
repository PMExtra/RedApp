<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { useFormat } from "@/shared/i18n";
import { AsyncState, Badge, RelativeTime } from "@/shared/ui";
import { useRetentionStatus } from "./queries";

/** The last automatic retention run and the next scheduled check. */
const props = defineProps<{ vendor: string; app: string }>();
const { t } = useI18n();
const format = useFormat();
const status = useRetentionStatus(
  () => props.vendor,
  () => props.app,
);
const lastRun = computed(() => status.data.value?.last_run ?? null);
const outcomeTone = { success: "success", failure: "danger", skip: "warning" } as const;
</script>

<template>
  <section
    class="flex flex-col gap-2 rounded-lg bg-surface-sunken p-4 text-sm"
    aria-labelledby="retention-status"
  >
    <h3 id="retention-status" class="font-semibold">{{ t("retention.status.title") }}</h3>
    <AsyncState
      :loading="status.isPending.value"
      :error="status.error.value"
      @retry="status.refetch()"
    >
      <p v-if="!lastRun" class="text-muted">{{ t("retention.status.never") }}</p>
      <div v-else class="flex flex-wrap items-center gap-x-3 gap-y-1">
        <Badge :tone="outcomeTone[lastRun.outcome]">
          {{ t(`retention.status.outcomes.${lastRun.outcome}`) }}
        </Badge>
        <RelativeTime :value="lastRun.attempted_at" />
        <span v-if="lastRun.reason">{{ t(`retention.status.reasons.${lastRun.reason}`) }}</span>
        <span>
          {{
            t("retention.status.removed", {
              count: lastRun.retired_versions,
              size: format.bytes(lastRun.logical_bytes),
            })
          }}
        </span>
        <span v-if="lastRun.succeeded_at" class="text-muted">
          {{ t("retention.status.lastSuccess") }}
          <RelativeTime :value="lastRun.succeeded_at" />
        </span>
      </div>
      <p class="text-muted">
        <template v-if="status.data.value?.next_check_at">
          {{ t("retention.status.next") }}
          <RelativeTime :value="status.data.value.next_check_at" />
        </template>
        <template v-else>{{ t("retention.status.notScheduled") }}</template>
      </p>
    </AsyncState>
  </section>
</template>
