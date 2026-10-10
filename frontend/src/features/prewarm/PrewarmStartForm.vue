<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { ChevronRight } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import {
  DurationInput,
  PathMatchInput,
  patternTooLong,
  type PathMatch,
} from "@/features/cache-policy";
import { isApiError } from "@/shared/api";
import { randomId } from "@/shared/lib";
import {
  Alert,
  Button,
  Checkbox,
  Field,
  FilePicker,
  Input,
  NumberInput,
  RadioGroup,
  Select,
  Textarea,
  type RadioOption,
  type SelectOption,
} from "@/shared/ui";
import SizeInput from "./SizeInput.vue";
import {
  usePrewarmActions,
  type PrewarmJob,
  type PrewarmLimits,
  type PrewarmOptions,
  type PrewarmStartRequest,
} from "./queries";

/**
 * A one-time prewarm task: a channel or version and platforms (release
 * applications) or paths, directory indexes, a path manifest and a filter
 * (HTTP cache applications), with task limits.
 */
const props = defineProps<{
  vendor: string;
  app: string;
  options: PrewarmOptions;
  disabled?: boolean;
}>();
const emit = defineEmits<{ started: [job: PrewarmJob] }>();
const { t } = useI18n();
const MAX_MANIFEST_BYTES = 2 * 1024 * 1024;
const release = computed(() => props.options.kind === "release");

// Release target.
const targetKind = ref<"channel" | "version">(
  props.options.channels.length ? "channel" : "version",
);
const channel = ref(props.options.channels[0] ?? "");
const version = ref("");
const platforms = ref<string[]>([]);
// HTTP cache selection.
const paths = ref("");
const indexes = ref("");
const manifestFiles = ref<File[]>([]);
const manifest = ref<{ name: string; text: string; lines: number } | null>(null);
const manifestError = ref<string>();
const filterEnabled = ref(false);
const filter = ref<PathMatch>({ type: "glob", pattern: "/**" });
const limits = ref<PrewarmLimits>({ ...props.options.default_limits });
const submitted = ref(false);

const targetOptions = computed<RadioOption[]>(() => [
  ...(props.options.channels.length
    ? [{ value: "channel", label: t("prewarm.start.channel") }]
    : []),
  { value: "version", label: t("prewarm.start.version") },
]);
const channelOptions = computed<SelectOption[]>(() =>
  props.options.channels.map((value) => ({ value, label: value })),
);
const targetKindValue = computed({
  get: () => targetKind.value,
  set: (value: string | undefined) => {
    if (value === "channel" || value === "version") targetKind.value = value;
  },
});

function lines(text: string): string[] {
  return text
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);
}

// Reading is asynchronous: a slow read of an earlier file must not replace a
// later choice (or a cleared one).
let manifestRead = 0;
watch(manifestFiles, async (files) => {
  const read = ++manifestRead;
  manifestError.value = undefined;
  manifest.value = null;
  const file = files[0];
  if (!file) return;
  if (file.size > MAX_MANIFEST_BYTES) {
    manifestError.value = t("prewarm.start.manifestTooLarge");
    return;
  }
  try {
    const text = new TextDecoder("utf-8", { fatal: true }).decode(await file.arrayBuffer());
    if (read === manifestRead)
      manifest.value = { name: file.name, text, lines: lines(text).length };
  } catch {
    if (read === manifestRead) manifestError.value = t("prewarm.start.manifestInvalid");
  }
});

function setLimit(key: "max_files" | "max_depth", value: number | null) {
  if (value !== null) limits.value = { ...limits.value, [key]: value };
}

function togglePlatform(id: string, checked: boolean | "indeterminate") {
  const next = new Set(platforms.value);
  if (checked === true) next.add(id);
  else next.delete(id);
  platforms.value = props.options.platforms.map((p) => p.id).filter((p) => next.has(p));
}

/** The request without `request_id`, or an error key. */
const request = computed<{ body: Record<string, unknown> } | { error: string }>(() => {
  if (release.value) {
    const target = targetKind.value === "channel" ? channel.value : version.value.trim();
    if (!target) return { error: "prewarm.start.targetRequired" };
    if (platforms.value.length === 0) return { error: "prewarm.start.platformsRequired" };
    return {
      body: {
        target,
        platforms: platforms.value,
        limits: limits.value,
      },
    };
  }
  const pathList = lines(paths.value);
  const indexList = lines(indexes.value);
  const match = filterEnabled.value ? filter.value : undefined;
  if (match && (!match.pattern || patternTooLong(match.pattern))) {
    return { error: "prewarm.start.filterInvalid" };
  }
  if (!pathList.length && !indexList.length && !manifest.value && !match) {
    return { error: "prewarm.start.selectionRequired" };
  }
  return {
    body: {
      ...(pathList.length ? { paths: pathList } : {}),
      ...(indexList.length ? { indexes: indexList } : {}),
      ...(manifest.value ? { manifest: manifest.value.text } : {}),
      ...(match ? { match } : {}),
      limits: limits.value,
    },
  };
});
const error = computed(() => ("error" in request.value ? t(request.value.error) : undefined));

// `request_id` is an idempotency key: resubmitting the same request (for
// example after a network error) returns the same job; any edit gets a new key.
const requestId = ref(randomId());
watch(
  () => JSON.stringify("body" in request.value ? request.value.body : null),
  () => {
    requestId.value = randomId();
  },
);

const { start } = usePrewarmActions(
  () => props.vendor,
  () => props.app,
);
const busyError = computed(() => isApiError(start.error.value, "PREWARM_BUSY"));

function submit() {
  submitted.value = true;
  if (!("body" in request.value) || props.disabled) return;
  const body = { ...request.value.body, request_id: requestId.value } as PrewarmStartRequest;
  start.mutate(body, {
    onSuccess: (job) => {
      requestId.value = randomId();
      submitted.value = false;
      emit("started", job);
    },
  });
}
</script>

<template>
  <form class="flex flex-col gap-4" novalidate @submit.prevent="submit">
    <template v-if="release">
      <fieldset class="flex flex-col gap-2">
        <legend class="mb-1 text-sm font-medium">{{ t("prewarm.start.target") }}</legend>
        <RadioGroup v-model="targetKindValue" :options="targetOptions" orientation="horizontal" />
        <Field
          v-if="targetKind === 'channel'"
          v-slot="{ control }"
          class="w-64"
          :label="t('prewarm.start.channel')"
          hide-label
        >
          <Select v-bind="control" v-model="channel" :options="channelOptions" />
        </Field>
        <Field
          v-else
          v-slot="{ control }"
          class="w-64"
          :label="t('prewarm.start.version')"
          :description="t('prewarm.start.versionHint')"
          hide-label
        >
          <Input
            v-bind="control"
            v-model="version"
            maxlength="128"
            spellcheck="false"
            autocomplete="off"
            placeholder="1.2.3"
          />
        </Field>
      </fieldset>
      <fieldset class="flex flex-col gap-2">
        <legend class="mb-1 text-sm font-medium">{{ t("prewarm.start.platforms") }}</legend>
        <div class="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-4">
          <Checkbox
            v-for="platform in options.platforms"
            :key="platform.id"
            :label="platform.name"
            :model-value="platforms.includes(platform.id)"
            @update:model-value="togglePlatform(platform.id, $event)"
          />
        </div>
      </fieldset>
    </template>

    <template v-else>
      <div class="grid gap-4 lg:grid-cols-2">
        <Field
          v-slot="{ control }"
          :label="t('prewarm.start.paths')"
          :description="t('prewarm.start.pathsHint')"
        >
          <Textarea
            v-bind="control"
            v-model="paths"
            monospace
            :rows="5"
            placeholder="/file.tar.gz"
          />
        </Field>
        <Field
          v-slot="{ control }"
          :label="t('prewarm.start.indexes')"
          :description="t('prewarm.start.indexesHint')"
        >
          <Textarea
            v-bind="control"
            v-model="indexes"
            monospace
            :rows="5"
            placeholder="/releases/"
          />
        </Field>
      </div>
      <div class="flex flex-col gap-1.5">
        <span id="prewarm-manifest-label" class="text-sm font-medium">
          {{ t("prewarm.start.manifest") }}
        </span>
        <FilePicker
          v-model="manifestFiles"
          accept=".txt,text/plain"
          aria-labelledby="prewarm-manifest-label"
        />
        <p class="text-xs text-muted">{{ t("prewarm.start.manifestHint") }}</p>
        <p v-if="manifest" class="text-xs" role="status">
          {{ t("prewarm.start.manifestLoaded", { name: manifest.name, count: manifest.lines }) }}
        </p>
        <p v-if="manifestError" class="text-xs text-danger" role="alert">{{ manifestError }}</p>
      </div>
      <div class="flex flex-col gap-3">
        <Checkbox v-model="filterEnabled" :label="t('prewarm.start.filter')" />
        <PathMatchInput v-if="filterEnabled" v-model="filter" :legend="t('prewarm.start.filter')" />
        <p class="text-xs text-muted">{{ t("prewarm.start.filterHint") }}</p>
      </div>
    </template>

    <details class="group">
      <summary
        class="inline-flex cursor-pointer items-center gap-1 rounded-sm text-sm font-medium focus-ring"
      >
        <ChevronRight class="size-4 transition-transform group-open:rotate-90" aria-hidden="true" />
        {{ t("prewarm.limits.title") }}
      </summary>
      <div class="mt-3 grid gap-4 sm:grid-cols-2">
        <Field v-slot="{ control }" :label="t('prewarm.limits.files')">
          <NumberInput
            v-bind="control"
            :model-value="limits.max_files"
            :min="1"
            :max="100000"
            :step="1"
            @update:model-value="setLimit('max_files', $event)"
          />
        </Field>
        <Field
          v-if="!release"
          v-slot="{ control }"
          :label="t('prewarm.limits.depth')"
          :description="t('prewarm.limits.depthHint')"
        >
          <NumberInput
            v-bind="control"
            :model-value="limits.max_depth"
            :min="0"
            :max="32"
            :step="1"
            @update:model-value="setLimit('max_depth', $event)"
          />
        </Field>
        <SizeInput
          v-model="limits.max_download_bytes"
          :label="t('prewarm.limits.bytes')"
          :description="t('prewarm.limits.bytesHint')"
          :max="1024 ** 4"
        />
        <DurationInput
          v-model="limits.max_duration_seconds"
          :label="t('prewarm.limits.duration')"
          :min="1"
          :max="86400"
        />
      </div>
    </details>

    <Alert v-if="busyError" tone="warning">{{ t("prewarm.start.busy") }}</Alert>
    <p v-if="submitted && error" class="text-sm text-danger" role="alert">{{ error }}</p>
    <div class="flex flex-wrap items-center gap-3">
      <Button type="submit" variant="primary" :loading="start.isPending.value" :disabled="disabled">
        {{ t("prewarm.start.submit") }}
      </Button>
      <span v-if="disabled" class="text-xs text-muted">{{ t("prewarm.start.running") }}</span>
    </div>
  </form>
</template>
