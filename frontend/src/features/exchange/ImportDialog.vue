<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { describeError } from "@/shared/api";
import { useFormat } from "@/shared/i18n";
import {
  Alert,
  Badge,
  Button,
  Checkbox,
  Dialog,
  FilePicker,
  ProgressBar,
  RelativeTime,
} from "@/shared/ui";
import { itemKey, serializeChoices, type ChoiceDraft } from "./choices";
import ImportItemCard from "./ImportItemCard.vue";
import { previewImport, useExecuteImport, type ImportPreview, type ImportResult } from "./queries";

/**
 * Configuration import: upload a ZIP or YAML package, review the preview and
 * decide per item, then execute. Any changed decision needs a new preview.
 * Packages that change usage instructions require explicit trust, because
 * instructions may run scripts on the public page.
 */
const open = defineModel<boolean>("open", { default: false });
const { t } = useI18n();
const format = useFormat();
const execute = useExecuteImport();

const files = ref<File[]>([]);
const preview = ref<ImportPreview | null>(null);
const drafts = ref<Record<string, ChoiceDraft>>({});
const changed = ref(false);
const trust = ref<boolean | "indeterminate">(false);
const uploading = ref(false);
const progress = ref<number | null>(null);
const failure = ref<{ message: string; requestId: string | null } | null>(null);
const result = ref<ImportResult | null>(null);
let controller: AbortController | undefined;

function reset(): void {
  controller?.abort();
  files.value = [];
  preview.value = null;
  drafts.value = {};
  changed.value = false;
  trust.value = false;
  uploading.value = false;
  failure.value = null;
  result.value = null;
}

watch(open, (value) => {
  if (value) reset();
  else controller?.abort();
});

// A different file starts over.
watch(files, () => {
  preview.value = null;
  drafts.value = {};
  changed.value = false;
  trust.value = false;
  failure.value = null;
});

const appCount = computed(
  () => preview.value?.items.filter((item) => item.kind === "app").length ?? 0,
);
const busy = computed(() => uploading.value || execute.isPending.value);
const canExecute = computed(() => {
  const current = preview.value;
  if (!current || changed.value || busy.value) return false;
  return current.ready && (!current.needs_instructions_trust || trust.value === true);
});

function draftOf(key: string): ChoiceDraft {
  return drafts.value[key] ?? {};
}

function setDraft(key: string, draft: ChoiceDraft): void {
  drafts.value = { ...drafts.value, [key]: draft };
  changed.value = true;
  trust.value = false;
}

async function requestPreview(): Promise<void> {
  const file = files.value[0];
  if (!file || busy.value) return;
  controller?.abort();
  controller = new AbortController();
  uploading.value = true;
  progress.value = null;
  failure.value = null;
  const choices = preview.value ? serializeChoices(preview.value.items, drafts.value) : [];
  try {
    preview.value = await previewImport(file, choices, {
      signal: controller.signal,
      onProgress: ({ loaded, total }) => {
        progress.value = total ? Math.round((loaded / total) * 100) : null;
      },
    });
    changed.value = false;
    trust.value = false;
  } catch (error) {
    if (error instanceof DOMException && error.name === "AbortError") return;
    const description = describeError(error);
    failure.value = {
      message: [description.message, description.detail].filter(Boolean).join(" "),
      requestId: description.requestId,
    };
  } finally {
    uploading.value = false;
  }
}

async function run(): Promise<void> {
  const current = preview.value;
  if (!current || !canExecute.value) return;
  try {
    result.value = await execute.mutateAsync({
      id: current.id,
      trust: current.needs_instructions_trust && trust.value === true,
    });
  } catch {
    // Reported by the global error handler (e.g. PREVIEW_STALE: upload again).
  }
}
</script>

<template>
  <Dialog
    v-model:open="open"
    size="xl"
    :title="t('exchange.import.title')"
    :description="t('exchange.import.description')"
    :persistent="busy"
  >
    <div v-if="result" class="flex flex-col gap-4">
      <Alert tone="success" :title="t('exchange.import.doneTitle')">
        {{ t("exchange.import.done", { count: result.items.length }, result.items.length) }}
      </Alert>
      <ul class="flex flex-col gap-1 text-sm" :aria-label="t('exchange.import.receipt')">
        <li v-for="item in result.items" :key="`${item.kind}:${item.key}`" class="flex gap-2">
          <Badge>{{ t(`exchange.import.kinds.${item.kind}`) }}</Badge>
          <span class="font-mono">{{ item.key }}</span>
          <span class="text-muted">{{
            t("exchange.import.revision", { revision: item.revision })
          }}</span>
        </li>
      </ul>
      <p class="text-sm text-muted">{{ t("exchange.import.disabledHint") }}</p>
    </div>

    <div v-else class="flex flex-col gap-5">
      <FilePicker v-model="files" accept=".zip,.yaml,.yml" :disabled="busy" />
      <p class="text-xs text-muted">{{ t("exchange.import.fileHint") }}</p>
      <ProgressBar v-if="uploading" :value="progress" :label="t('exchange.import.uploading')" />
      <Alert v-if="failure" tone="danger">
        {{ failure.message }}
        <span v-if="failure.requestId" class="mt-1 block text-xs text-muted">
          {{ t("common.requestId") }}: <code>{{ failure.requestId }}</code>
        </span>
      </Alert>

      <template v-if="preview">
        <div class="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted">
          <span>
            {{ t("exchange.import.items", { count: format.number(preview.items.length) }) }}
          </span>
          <span>
            {{ t("exchange.import.expires") }} <RelativeTime :value="preview.expires_at" />
          </span>
          <span class="font-mono">SHA-256 {{ preview.digest.slice(0, 12) }}…</span>
        </div>
        <div class="flex flex-col gap-3">
          <ImportItemCard
            v-for="item in preview.items"
            :key="itemKey(item)"
            :model-value="draftOf(itemKey(item))"
            :item="item"
            :renameable="appCount === 1"
            :disabled="busy"
            @update:model-value="setDraft(itemKey(item), $event)"
          />
        </div>
        <Alert v-if="changed" tone="warning" :title="t('exchange.import.changedTitle')">
          {{ t("exchange.import.changed") }}
        </Alert>
        <Alert v-else-if="!preview.ready" tone="warning">
          {{ t("exchange.import.notReady") }}
        </Alert>
        <div
          v-if="preview.needs_instructions_trust"
          class="flex flex-col gap-2 rounded-lg border border-warning/40 bg-warning-soft p-4"
        >
          <p class="text-sm">{{ t("exchange.import.trustWarning") }}</p>
          <Checkbox
            v-model="trust"
            :label="t('exchange.import.trust')"
            :disabled="busy || changed"
          />
        </div>
      </template>
    </div>

    <template #footer>
      <template v-if="result">
        <Button variant="primary" @click="open = false">{{ t("common.actions.close") }}</Button>
      </template>
      <template v-else>
        <Button :disabled="busy" @click="open = false">{{ t("common.actions.cancel") }}</Button>
        <Button
          :variant="preview && !changed ? 'secondary' : 'primary'"
          :disabled="files.length === 0 || busy"
          :loading="uploading"
          @click="requestPreview"
        >
          {{ preview ? t("exchange.import.updatePreview") : t("exchange.import.preview") }}
        </Button>
        <Button
          v-if="preview"
          variant="primary"
          :disabled="!canExecute"
          :loading="execute.isPending.value"
          @click="run"
        >
          {{ t("exchange.import.execute") }}
        </Button>
      </template>
    </template>
  </Dialog>
</template>
