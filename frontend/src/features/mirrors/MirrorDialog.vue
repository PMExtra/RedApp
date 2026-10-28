<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { isApiError } from "@/shared/api";
import { useProviders, type App } from "@/features/directory";
import { useLocalized } from "@/shared/i18n";
import { Button, Dialog } from "@/shared/ui";
import MirrorTargetRow from "./MirrorTargetRow.vue";
import {
  useCreateMirror,
  useRecheckTarget,
  useRefreshAfterMirroring,
  type UpstreamCatalogItem,
} from "./queries";
import { mirrorErrorText, splitKey, type TargetStatus } from "./target";

/**
 * Mirrors the selected upstream applications one by one. Each row chooses
 * its local IDs (default: the upstream's) and must explicitly adopt an
 * existing local application. Rows that failed stay editable and can be
 * submitted again; successful rows link to the new application.
 */
const open = defineModel<boolean>("open", { default: false });
const props = defineProps<{ upstream: string; items: UpstreamCatalogItem[] }>();
const emit = defineEmits<{ mirrored: [keys: string[]] }>();
const { t } = useI18n();
const localized = useLocalized();
const providers = useProviders();
const mirror = useCreateMirror(() => props.upstream);
const refresh = useRefreshAfterMirroring();
const recheck = useRecheckTarget();

interface Row {
  remote: UpstreamCatalogItem;
  vendor: string;
  app: string;
  adopt: boolean;
  status: TargetStatus;
  done?: App;
  error?: string;
}
const rows = ref<Row[]>([]);
const submitted = ref(false);
const running = ref(false);

// Every opening starts from the current selection with the upstream's IDs.
watch(open, (isOpen) => {
  if (!isOpen) return;
  submitted.value = false;
  rows.value = props.items.map((remote) => ({
    remote,
    ...splitKey(remote.key),
    adopt: false,
    status: { kind: "checking" },
  }));
});

function providerName(key: string): string {
  const provider = providers.data.value?.items.find((item) => item.key === key);
  return provider ? localized(provider.name) || key : key;
}

function ready(row: Row): boolean {
  return row.status.kind === "new" || (row.status.kind === "existing" && row.adopt);
}

const pendingRows = computed(() => rows.value.filter((row) => !row.done));
const doneCount = computed(() => rows.value.length - pendingRows.value.length);
const retrying = computed(() => pendingRows.value.some((row) => row.error));
const checking = computed(() => pendingRows.value.some((row) => row.status.kind === "checking"));

async function submit(): Promise<void> {
  submitted.value = true;
  const todo = pendingRows.value;
  if (todo.length === 0 || !todo.every(ready)) return;
  running.value = true;
  const mirrored: App[] = [];
  for (const row of todo) {
    row.error = undefined;
    const existing = row.status.kind === "existing" ? row.status.existing : undefined;
    try {
      const app = await mirror.mutateAsync({
        remote_key: row.remote.key,
        vendor: row.vendor.trim(),
        app: row.app.trim(),
        ...(existing ? { revision: existing.revision } : {}),
      });
      row.done = app;
      mirrored.push(app);
    } catch (error) {
      row.error = mirrorErrorText(error);
      // The target changed meanwhile (adopted elsewhere, created, deleted):
      // read it again and ask for a new confirmation before retrying.
      if (
        isApiError(error, "REVISION_CONFLICT") ||
        isApiError(error, "ALREADY_EXISTS") ||
        isApiError(error, "ENTITY_DELETED") ||
        isApiError(error, "APPLICATION_NOT_FOUND")
      ) {
        row.adopt = false;
        void recheck(row.vendor.trim(), row.app.trim());
      }
    }
  }
  running.value = false;
  submitted.value = false;
  // Failures may come from a catalog that changed meanwhile: refresh it too.
  refresh(mirrored);
  if (mirrored.length > 0) {
    emit(
      "mirrored",
      rows.value.filter((row) => row.done).map((row) => row.remote.key),
    );
  }
}
</script>

<template>
  <Dialog
    v-model:open="open"
    :title="t('mirrors.mirror.title')"
    :description="t('mirrors.mirror.description')"
    size="lg"
    :persistent="running"
  >
    <form id="mirror-form" class="flex flex-col gap-4" novalidate @submit.prevent="submit">
      <ul class="flex flex-col gap-3" :aria-label="t('mirrors.mirror.caption')">
        <MirrorTargetRow
          v-for="row in rows"
          :key="row.remote.key"
          v-model:vendor="row.vendor"
          v-model:app="row.app"
          v-model:adopt="row.adopt"
          :remote="row.remote"
          :provider-name="providerName"
          :done="row.done"
          :error="row.error"
          :submitted="submitted"
          :disabled="running"
          @status="row.status = $event"
        />
      </ul>
      <p v-if="doneCount > 0" class="text-sm text-muted" role="status">
        {{ t("mirrors.mirror.summary", { done: doneCount, total: rows.length }) }}
      </p>
    </form>
    <template #footer>
      <Button :disabled="running" @click="open = false">
        {{ t(pendingRows.length === 0 ? "common.actions.close" : "common.actions.cancel") }}
      </Button>
      <Button
        v-if="pendingRows.length > 0"
        type="submit"
        form="mirror-form"
        variant="primary"
        :loading="running"
        :disabled="checking"
      >
        {{ t(retrying ? "mirrors.mirror.retry" : "mirrors.mirror.submit") }}
      </Button>
    </template>
  </Dialog>
</template>
