<script setup lang="ts">
import { computed } from "vue";
import { RefreshCw, Unlink } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import { isUpstreamRef, useAppConfiguration } from "@/features/configuration";
import type { App } from "@/features/directory";
import { hasTranslation } from "@/shared/i18n";
import { confirm, toast } from "@/shared/lib";
import { Alert, AsyncState, Badge, Button, Card, RelativeTime } from "@/shared/ui";
import { useAppMirror, useSyncAppMirror, useUnbindAppMirror } from "./queries";

/**
 * The upstream a mirrored application follows (its configuration links the
 * template `upstream:<uid>`), its state and the actions "check now" and
 * "stop mirroring". Renders nothing for other applications.
 */
const props = defineProps<{ app: App }>();
const { t } = useI18n();
const vendor = () => props.app.vendor_id;
const appId = () => props.app.id;
const configuration = useAppConfiguration(vendor, appId);
const mirrored = computed(() => isUpstreamRef(configuration.data.value?.template_ref));
const mirror = useAppMirror(vendor, appId, mirrored);
const sync = useSyncAppMirror(vendor, appId);
const unbind = useUnbindAppMirror();
const readOnly = computed(() => props.app.deleted_at !== null);

const lastError = computed(() => {
  const code = mirror.data.value?.error;
  if (!code) return null;
  return hasTranslation(`errors.codes.${code}`) ? t(`errors.codes.${code}`) : code;
});

function check(): void {
  sync.mutate(undefined, {
    onSuccess: () => toast({ tone: "success", title: t("mirrors.section.checkedToast") }),
  });
}

async function stop(): Promise<void> {
  const confirmed = await confirm({
    title: t("mirrors.section.unbindTitle", { key: props.app.key }),
    description: t("mirrors.section.unbindWarning"),
    confirmLabel: t("mirrors.section.unbind"),
    tone: "danger",
  });
  if (!confirmed) return;
  try {
    await unbind.mutateAsync(props.app);
  } catch {
    return; // Reported globally or as a conflict notice.
  }
  toast({ tone: "success", title: t("mirrors.section.unbound", { key: props.app.key }) });
}
</script>

<template>
  <Card
    v-if="mirrored"
    :title="t('mirrors.section.title')"
    :description="t('mirrors.section.description')"
  >
    <AsyncState
      :loading="mirror.isPending.value"
      :error="mirror.data.value ? undefined : mirror.error.value"
      @retry="mirror.refetch()"
    >
      <div v-if="mirror.data.value" class="flex flex-col gap-4">
        <Alert v-if="!mirror.data.value.available" tone="warning">
          {{ t("mirrors.section.goneHint") }}
        </Alert>
        <dl class="grid grid-cols-1 gap-x-6 gap-y-2 text-sm sm:grid-cols-[auto_1fr]">
          <dt class="text-muted">{{ t("mirrors.section.upstream") }}</dt>
          <dd class="flex flex-wrap items-baseline gap-x-2">
            <RouterLink
              :to="{ name: 'admin-upstream', params: { upstream: mirror.data.value.upstream.uid } }"
              class="rounded-sm font-medium text-primary hover:underline focus-ring"
            >
              {{ mirror.data.value.upstream.name }}
            </RouterLink>
            <span class="font-mono text-xs text-muted">{{
              mirror.data.value.upstream.origin
            }}</span>
          </dd>
          <dt class="text-muted">{{ t("mirrors.section.remoteKey") }}</dt>
          <dd class="font-mono">{{ mirror.data.value.remote_key }}</dd>
          <dt class="text-muted">{{ t("mirrors.section.availability") }}</dt>
          <dd>
            <Badge v-if="mirror.data.value.available" tone="success">
              {{ t("mirrors.section.available") }}
            </Badge>
            <Badge v-else tone="warning">{{ t("mirrors.section.gone") }}</Badge>
          </dd>
          <dt class="text-muted">{{ t("mirrors.section.synced") }}</dt>
          <dd><RelativeTime :value="mirror.data.value.synced_at" /></dd>
          <dt class="text-muted">{{ t("mirrors.section.checked") }}</dt>
          <dd><RelativeTime :value="mirror.data.value.checked_at" /></dd>
          <dt class="text-muted">{{ t("mirrors.section.lastError") }}</dt>
          <dd :class="lastError ? 'text-danger' : 'text-muted'">
            {{ lastError ?? t("mirrors.section.noError") }}
          </dd>
        </dl>
        <div v-if="!readOnly" class="flex flex-wrap gap-2">
          <Button :loading="sync.isPending.value" :disabled="unbind.isPending.value" @click="check">
            <RefreshCw aria-hidden="true" /> {{ t("mirrors.section.check") }}
          </Button>
          <Button
            variant="danger"
            :loading="unbind.isPending.value"
            :disabled="sync.isPending.value"
            @click="stop"
          >
            <Unlink aria-hidden="true" /> {{ t("mirrors.section.unbind") }}
          </Button>
        </div>
      </div>
    </AsyncState>
  </Card>
</template>
