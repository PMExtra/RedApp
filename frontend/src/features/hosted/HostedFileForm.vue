<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { isApiError } from "@/shared/api";
import { useDirtyGuard } from "@/shared/forms";
import { toast } from "@/shared/lib";
import {
  Alert,
  Button,
  Card,
  Field,
  FilePicker,
  Input,
  ProgressBar,
  RadioGroup,
  type RadioOption,
} from "@/shared/ui";
import type { HostedFile } from "./queries";
import { useFileTransfer } from "./transfer";

/**
 * Adds a file by upload or one-time URL import, or replaces the file version
 * chosen in the list (`replacing`). Emits `update:busy` while transferring.
 */
const props = defineProps<{ vendor: string; app: string }>();
const replacing = defineModel<HostedFile | null>("replacing", { default: null });
const emit = defineEmits<{ "update:busy": [busy: boolean] }>();
const { t } = useI18n();
// A literal URL in a template attribute confuses vue-tsc; bind it.
const URL_PLACEHOLDER = "https://downloads.example.com/setup.exe";
/** HostedImportRequest.url maxLength. */
const MAX_URL_LENGTH = 8192;

const mode = ref<"upload" | "url">("upload");
const path = ref("");
const url = ref("");
const chosen = ref<File[]>([]);
const submitted = ref(false);
const conflict = ref(false);
const transfer = useFileTransfer(
  () => props.vendor,
  () => props.app,
);
const busy = transfer.active;
watch(busy, (value) => emit("update:busy", value));
useDirtyGuard(
  () => busy.value || Boolean(path.value.trim() || url.value.trim() || chosen.value.length),
);

const modeOptions = computed<RadioOption[]>(() => [
  { value: "upload", label: t("hosted.add.upload") },
  { value: "url", label: t("hosted.add.import") },
]);
const modeValue = computed({
  get: () => mode.value,
  set: (value: string | undefined) => {
    if (value === "upload" || value === "url") mode.value = value;
  },
});
watch(chosen, (list) => {
  const file = list[0];
  if (file && !path.value && !replacing.value) path.value = file.name;
});
watch(replacing, (file) => {
  if (!file) return;
  path.value = file.path;
  conflict.value = false;
});

// HostedImportRequest.url: absolute http(s), no credentials or fragment; the
// query is allowed.
function importUrlValid(value: string): boolean {
  if (value.length > MAX_URL_LENGTH || !/^https?:\/\/\S+$/i.test(value) || value.includes("#")) {
    return false;
  }
  try {
    const parsed = new URL(value);
    return parsed.host !== "" && parsed.username === "" && parsed.password === "";
  } catch {
    return false;
  }
}

const normalizedPath = computed(() => path.value.trim().replace(/^\/+/, ""));
const pathError = computed(() => {
  if (!submitted.value) return undefined;
  if (!normalizedPath.value) return t("hosted.add.pathRequired");
  return new TextEncoder().encode(normalizedPath.value).length > 4096
    ? t("hosted.add.pathTooLong")
    : undefined;
});
const urlError = computed(() => {
  if (!submitted.value || mode.value !== "url") return undefined;
  return importUrlValid(url.value.trim()) ? undefined : t("hosted.add.urlInvalid");
});
const fileError = computed(() =>
  submitted.value && mode.value === "upload" && chosen.value.length === 0
    ? t("hosted.add.fileRequired")
    : undefined,
);
// Announced once per state change, not on every progress update.
const announcement = computed(() => {
  if (!busy.value) return "";
  return transfer.committing.value ? t("hosted.progress.committing") : t("hosted.progress.started");
});

function resetForm() {
  replacing.value = null;
  path.value = "";
  url.value = "";
  chosen.value = [];
  submitted.value = false;
}

async function submit() {
  submitted.value = true;
  conflict.value = false;
  if (pathError.value || urlError.value || fileError.value || busy.value) return;
  const file = chosen.value[0];
  try {
    const saved = await transfer.run({
      path: normalizedPath.value,
      source:
        mode.value === "upload" && file
          ? { kind: "upload", file }
          : { kind: "url", url: url.value.trim() },
      expectedId: replacing.value?.id ?? null,
    });
    if (saved) {
      toast({ tone: "success", title: t("hosted.add.saved", { path: saved.path }) });
      resetForm();
    } else {
      toast({ tone: "info", title: t("hosted.progress.cancelled") });
    }
  } catch (error) {
    if (isApiError(error, "TRANSFER_CANCELLED")) {
      toast({ tone: "info", title: t("hosted.progress.cancelled") });
    } else if (isApiError(error, "FILE_CONFLICT")) {
      conflict.value = true;
    }
  }
}
</script>

<template>
  <Card
    :title="replacing ? t('hosted.add.replaceTitle') : t('hosted.add.title')"
    :description="t('hosted.add.description')"
  >
    <form class="flex flex-col gap-4" novalidate @submit.prevent="submit">
      <Alert v-if="replacing" tone="info">
        {{ t("hosted.add.replacing", { path: replacing.path }) }}
        <template #actions>
          <Button size="sm" :disabled="busy" @click="resetForm">
            {{ t("hosted.add.cancelReplace") }}
          </Button>
        </template>
      </Alert>
      <Alert v-if="conflict" tone="warning" :title="t('hosted.add.conflictTitle')">
        {{ t("hosted.add.conflict") }}
      </Alert>
      <fieldset class="flex flex-col gap-4" :disabled="busy">
        <fieldset class="flex flex-col gap-2">
          <legend class="mb-1 text-sm font-medium">{{ t("hosted.add.source") }}</legend>
          <RadioGroup v-model="modeValue" :options="modeOptions" orientation="horizontal" />
        </fieldset>
        <Field
          v-if="mode === 'upload'"
          v-slot="{ control }"
          :label="t('hosted.add.file')"
          :error="fileError"
          required
        >
          <FilePicker v-bind="control" v-model="chosen" :disabled="busy" />
        </Field>
        <Field
          v-else
          v-slot="{ control }"
          :label="t('hosted.add.url')"
          :description="t('hosted.add.urlHint')"
          :error="urlError"
          required
        >
          <Input
            v-bind="control"
            v-model="url"
            type="url"
            inputmode="url"
            autocomplete="off"
            spellcheck="false"
            :maxlength="MAX_URL_LENGTH"
            :placeholder="URL_PLACEHOLDER"
          />
        </Field>
        <Field
          v-slot="{ control }"
          :label="t('hosted.add.path')"
          :description="t('hosted.add.pathHint')"
          :error="pathError"
          required
        >
          <Input
            v-bind="control"
            v-model="path"
            class="font-mono"
            autocomplete="off"
            spellcheck="false"
            maxlength="4096"
            placeholder="tools/setup.exe"
            :readonly="replacing !== null"
          />
        </Field>
      </fieldset>
      <p class="sr-only" role="status">{{ announcement }}</p>
      <div v-if="busy" class="flex flex-col gap-2 rounded-lg bg-surface-sunken p-3">
        <ProgressBar :value="transfer.progress.value.value" :label="t('hosted.progress.label')" />
        <div class="flex flex-wrap items-center justify-between gap-2 text-sm">
          <span class="tabular-nums">{{ transfer.progress.value.text }}</span>
          <Button
            v-if="!transfer.committing.value"
            size="sm"
            :loading="transfer.cancelling.value"
            @click="transfer.cancel()"
          >
            {{ t("hosted.progress.cancel") }}
          </Button>
        </div>
      </div>
      <div class="flex flex-wrap gap-2">
        <Button type="submit" variant="primary" :loading="busy">
          {{ replacing ? t("hosted.add.replace") : t("hosted.add.save") }}
        </Button>
        <Button v-if="!busy && !replacing" @click="resetForm">
          {{ t("hosted.add.clear") }}
        </Button>
      </div>
    </form>
  </Card>
</template>
