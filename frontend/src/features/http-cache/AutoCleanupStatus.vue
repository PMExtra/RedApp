<script setup lang="ts">
import { RefreshCw } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { useFormat } from "@/shared/i18n";
import { AsyncState, Badge, Card, IconButton, RelativeTime } from "@/shared/ui";
import { useAutoCleanupStatus } from "./queries";

/** Service-wide status of automatic HTTP cache cleanup (all applications). */
const { t } = useI18n();
const format = useFormat();
const status = useAutoCleanupStatus();
</script>

<template>
  <Card :title="t('httpCache.auto.title')" :description="t('httpCache.auto.description')">
    <template #actions>
      <IconButton
        variant="secondary"
        :label="t('httpCache.auto.reload')"
        :loading="status.isFetching.value"
        @click="status.refetch()"
      >
        <RefreshCw aria-hidden="true" />
      </IconButton>
    </template>
    <AsyncState
      :loading="status.isPending.value"
      :error="status.error.value"
      @retry="status.refetch()"
    >
      <div v-if="status.data.value" class="flex flex-col gap-3 text-sm">
        <div>
          <Badge :tone="status.data.value.running ? 'info' : 'neutral'">
            {{ status.data.value.running ? t("httpCache.auto.running") : t("httpCache.auto.idle") }}
          </Badge>
        </div>
        <dl class="grid grid-cols-1 gap-3 sm:grid-cols-3">
          <div>
            <dt class="text-xs text-muted">{{ t("httpCache.auto.lastAttempt") }}</dt>
            <dd>
              <RelativeTime
                :value="status.data.value.last_attempt_at"
                :fallback="t('httpCache.auto.never')"
              />
            </dd>
          </div>
          <div>
            <dt class="text-xs text-muted">{{ t("httpCache.auto.lastSuccess") }}</dt>
            <dd>
              <RelativeTime
                :value="status.data.value.last_success_at"
                :fallback="t('httpCache.auto.never')"
              />
            </dd>
          </div>
          <div>
            <dt class="text-xs text-muted">{{ t("httpCache.auto.lastError") }}</dt>
            <dd>
              <RelativeTime
                :value="status.data.value.last_error_at"
                :fallback="t('httpCache.auto.never')"
              />
            </dd>
          </div>
        </dl>
        <p>
          {{
            t("httpCache.auto.lastPass", {
              apps: format.number(status.data.value.configured_apps),
              scanned: format.number(status.data.value.scanned_files),
              retired: format.number(status.data.value.retired_files),
              size: format.bytes(status.data.value.retired_bytes),
              accessed: format.number(status.data.value.skipped_accessed),
              changed: format.number(status.data.value.skipped_changed),
            })
          }}
        </p>
        <p class="text-muted">
          {{
            t("httpCache.auto.totals", {
              passes: format.number(status.data.value.passes_total),
              failures: format.number(status.data.value.failures_total),
            })
          }}
        </p>
        <p v-if="status.data.value.last_error" class="font-mono text-xs text-danger">
          {{ status.data.value.last_error }}
        </p>
      </div>
    </AsyncState>
  </Card>
</template>
