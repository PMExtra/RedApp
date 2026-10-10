<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import {
  FieldReset,
  useAppConfiguration,
  useAppConfigurationPatch,
  useOverlayDraft,
} from "@/features/configuration";
import type { Schema } from "@/shared/api";
import { useDirtyGuard } from "@/shared/forms";
import { toast } from "@/shared/lib";
import { Alert, AsyncState, Button, Checkbox, RevisionConflictAlert, Switch } from "@/shared/ui";
import type { PrewarmOptions } from "./queries";

type Policy = Schema<"PrewarmPolicy">;
type Channel = Policy["channels"][number];
type Platform = Policy["platforms"][number];

/**
 * Automatic prewarm of a release application: every 15 minutes the versions
 * the selected channels point to are downloaded for the selected platforms.
 */
const props = defineProps<{
  vendor: string;
  app: string;
  options: PrewarmOptions;
  readOnly?: boolean;
}>();
const { t } = useI18n();
const POLICY_CHANNELS: readonly Channel[] = ["latest", "stable"];

const configuration = useAppConfiguration(
  () => props.vendor,
  () => props.app,
);
const overlay = useOverlayDraft<{ prewarm: Policy }>(configuration.data, (spec) =>
  spec?.prewarm ? { prewarm: spec.prewarm } : undefined,
);
const { draft } = overlay;
const save = useAppConfigurationPatch(
  () => props.vendor,
  () => props.app,
  configuration.data,
);
useDirtyGuard(overlay.dirty);

const channels = computed(() =>
  POLICY_CHANNELS.filter(
    (channel) =>
      props.options.channels.includes(channel) || draft.value?.prewarm.channels.includes(channel),
  ),
);

function update(change: Partial<Policy>) {
  if (draft.value) draft.value = { prewarm: { ...draft.value.prewarm, ...change } };
}

function toggle<T extends string>(list: readonly T[], value: T, on: boolean | "indeterminate") {
  return on === true ? [...new Set([...list, value])] : list.filter((item) => item !== value);
}

const enabled = computed({
  get: () => draft.value?.prewarm.enabled ?? false,
  set: (value: boolean) => {
    update({ enabled: value });
  },
});
const error = computed(() => {
  const policy = draft.value?.prewarm;
  if (!policy?.enabled) return undefined;
  if (!policy.channels.length || !policy.platforms.length) return t("prewarm.auto.incomplete");
  return undefined;
});

function submit() {
  const body = overlay.patch.value;
  if (!body || error.value || props.readOnly) return;
  save.mutate(body, {
    onSuccess: () => {
      overlay.reset();
      toast({ tone: "success", title: t("prewarm.auto.saved") });
    },
  });
}

async function reload() {
  await save.reload();
  overlay.reset();
}
</script>

<template>
  <AsyncState
    :loading="configuration.isPending.value"
    :error="configuration.error.value"
    @retry="configuration.refetch()"
  >
    <form v-if="draft" class="flex flex-col gap-4" novalidate @submit.prevent="submit">
      <RevisionConflictAlert v-if="save.hasConflict.value" @reload="reload" />
      <fieldset class="flex flex-col gap-4" :disabled="readOnly || save.isPending.value">
        <div class="flex items-start gap-3">
          <Switch id="prewarm-auto" v-model="enabled" aria-describedby="prewarm-auto-hint" />
          <div class="flex min-w-0 flex-1 flex-col gap-1">
            <label for="prewarm-auto" class="text-sm font-medium">{{
              t("prewarm.auto.enabled")
            }}</label>
            <p id="prewarm-auto-hint" class="text-xs text-muted">{{ t("prewarm.auto.hint") }}</p>
          </div>
          <FieldReset
            :linked="(configuration.data.value?.template_ref ?? null) !== null"
            :origin="configuration.data.value?.fields.prewarm"
            :modified="overlay.modified('prewarm')"
            @reset="overlay.restore('prewarm')"
          />
        </div>
        <fieldset class="flex flex-col gap-2">
          <legend class="mb-1 text-sm font-medium">{{ t("prewarm.auto.channels") }}</legend>
          <div class="flex flex-wrap gap-4">
            <Checkbox
              v-for="channel in channels"
              :key="channel"
              :label="channel"
              :model-value="draft.prewarm.channels.includes(channel)"
              @update:model-value="
                update({ channels: toggle(draft.prewarm.channels, channel, $event) })
              "
            />
          </div>
        </fieldset>
        <fieldset class="flex flex-col gap-2">
          <legend class="mb-1 text-sm font-medium">{{ t("prewarm.auto.platforms") }}</legend>
          <div class="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-4">
            <Checkbox
              v-for="platform in options.platforms"
              :key="platform.id"
              :label="platform.name"
              :model-value="draft.prewarm.platforms.includes(platform.id)"
              @update:model-value="
                update({
                  platforms: toggle<Platform>(draft.prewarm.platforms, platform.id, $event),
                })
              "
            />
          </div>
        </fieldset>
      </fieldset>
      <Alert v-if="error" tone="warning">{{ error }}</Alert>
      <div v-if="!readOnly" class="flex flex-wrap gap-2">
        <Button
          type="submit"
          variant="primary"
          :loading="save.isPending.value"
          :disabled="!overlay.dirty.value || Boolean(error)"
        >
          {{ t("prewarm.auto.save") }}
        </Button>
        <Button :disabled="!overlay.dirty.value || save.isPending.value" @click="overlay.reset()">
          {{ t("prewarm.auto.discard") }}
        </Button>
      </div>
    </form>
  </AsyncState>
</template>
