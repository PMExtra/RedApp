<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useMutation } from "@tanstack/vue-query";
import { useI18n } from "vue-i18n";
import { api, unwrap, type Schema } from "@/shared/api";
import {
  isUpstreamRef,
  useAppConfiguration,
  useAppConfigurationPatch,
  OverlayFormActions,
} from "@/features/configuration";
import { useDirtyGuard } from "@/shared/forms";
import {
  Alert,
  AsyncState,
  Button,
  Card,
  Checkbox,
  Field,
  Input,
  Textarea,
  RevisionConflictAlert,
} from "@/shared/ui";
const props = defineProps<{ vendor: string; app: string; readOnly?: boolean }>();
const { t } = useI18n();
const configuration = useAppConfiguration(
  () => props.vendor,
  () => props.app,
);
const save = useAppConfigurationPatch(
  () => props.vendor,
  () => props.app,
  configuration.data,
);
// A mirror's `json_release` belongs to its upstream: it can be previewed, not edited.
const mirrored = computed(() => isUpstreamRef(configuration.data.value?.template_ref));
const locked = computed(() => props.readOnly || mirrored.value);
const text = ref("");
const baseline = ref("");
const channel = ref("latest");
const prewarmKeys = ref<string[]>([]);
const dirty = computed(() => text.value !== baseline.value);
useDirtyGuard(dirty);
const parsed = computed(() => {
  try {
    const result: unknown = JSON.parse(text.value);
    return result && typeof result === "object" && !Array.isArray(result)
      ? (result as Schema<"JSONReleaseConfig">)
      : undefined;
  } catch {
    return undefined;
  }
});
const preview = useMutation({
  mutationFn: () => {
    const config = parsed.value;
    if (!config) throw new Error(t("jsonRelease.invalid"));
    return unwrap(
      api.POST("/admin/api/apps/{vendor}/{app}/release-preview", {
        params: { path: { vendor: props.vendor, app: props.app } },
        body: { config, channel: channel.value },
      }),
    );
  },
});
function reset() {
  text.value = baseline.value = JSON.stringify(
    configuration.data.value?.effective.json_release ?? {},
    null,
    2,
  );
  preview.reset();
}
watch([text, channel], () => preview.reset());
watch(preview.data, () => {
  const name = channel.value === "latest" ? parsed.value?.default_channel : channel.value;
  prewarmKeys.value = [...(parsed.value?.prewarm?.find((p) => p.channel === name)?.keys ?? [])];
});
function toggleKey(key: string, enabled: boolean | "indeterminate") {
  prewarmKeys.value =
    enabled === true
      ? [...prewarmKeys.value.filter((k) => k !== key), key]
      : prewarmKeys.value.filter((k) => k !== key);
}
function applyPrewarm() {
  const config = parsed.value;
  if (!config || locked.value) return;
  const name = channel.value === "latest" ? config.default_channel : channel.value;
  const prewarm = (config.prewarm ?? []).filter((p) => p.channel !== name);
  if (prewarmKeys.value.length) prewarm.push({ channel: name, keys: [...prewarmKeys.value] });
  text.value = JSON.stringify({ ...config, prewarm }, null, 2);
}
function submit() {
  if (!parsed.value || locked.value) return;
  save.mutate({ set: { json_release: parsed.value } }, { onSuccess: reset });
}
async function reload() {
  await save.reload();
  reset();
}
watch(
  configuration.data,
  () => {
    if (!dirty.value) reset();
  },
  { immediate: true },
);
</script>
<template>
  <Card :title="t('jsonRelease.title')" :description="t('jsonRelease.trust')">
    <AsyncState
      :loading="configuration.isPending.value"
      :error="configuration.error.value"
      @retry="configuration.refetch()"
    >
      <form v-if="configuration.data.value" class="flex flex-col gap-4" @submit.prevent="submit">
        <Alert v-if="mirrored" tone="info">{{ t("jsonRelease.mirrored") }}</Alert>
        <RevisionConflictAlert v-if="save.hasConflict.value" @reload="reload" />
        <Field
          :label="t('jsonRelease.config')"
          :error="parsed ? undefined : t('jsonRelease.invalid')"
          ><template #default="{ control }"
            ><Textarea
              v-bind="control"
              v-model="text"
              :rows="18"
              monospace
              :readonly="mirrored || undefined"
              :disabled="readOnly || save.isPending.value" /></template
        ></Field>
        <div class="flex flex-wrap items-end gap-3">
          <Field :label="t('jsonRelease.channel')"
            ><template #default="{ control }"><Input v-bind="control" v-model="channel" /></template
          ></Field>
          <Button :disabled="!parsed || preview.isPending.value" @click="preview.mutate()">{{
            t("jsonRelease.preview")
          }}</Button>
        </div>
        <AsyncState
          :loading="preview.isPending.value"
          :error="preview.error.value"
          @retry="preview.mutate()"
        >
          <pre
            v-if="preview.data.value"
            class="max-h-96 overflow-auto whitespace-pre-wrap break-all text-xs"
            :aria-label="t('jsonRelease.result')"
            >{{ JSON.stringify(preview.data.value, null, 2) }}</pre>
        </AsyncState>
        <fieldset v-if="preview.data.value && !locked" class="flex flex-col gap-3">
          <legend class="font-medium">{{ t("jsonRelease.prewarm") }}</legend>
          <p class="text-sm text-muted">{{ t("jsonRelease.prewarmHint") }}</p>
          <Checkbox
            v-for="artifact in preview.data.value.artifacts"
            :key="artifact.key"
            :model-value="prewarmKeys.includes(artifact.key)"
            :label="
              [
                artifact.os,
                artifact.arch,
                artifact.format,
                artifact.scope,
                artifact.variant,
                artifact.key,
              ]
                .filter(Boolean)
                .join(' · ')
            "
            @update:model-value="toggleKey(artifact.key, $event)"
          />
          <Button @click="applyPrewarm">{{ t("jsonRelease.applyPrewarm") }}</Button>
        </fieldset>
        <OverlayFormActions
          v-if="!locked"
          :dirty="dirty"
          :saving="save.isPending.value"
          :disabled="!dirty || !parsed"
          @discard="reset"
        />
      </form>
    </AsyncState>
  </Card>
</template>
