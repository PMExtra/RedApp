<script setup lang="ts">
import { computed, watch } from "vue";
import { useForm } from "vee-validate";
import { useI18n } from "vue-i18n";
import { z } from "zod";
import { FormField, formError, useDirtyGuard, zodSchema } from "@/shared/forms";
import { toast } from "@/shared/lib";
import {
  AsyncState,
  Badge,
  Button,
  Card,
  CopyButton,
  Input,
  RevisionConflictAlert,
} from "@/shared/ui";
import { usePublicUrlSettings, useSavePublicUrl, type PublicUrlState } from "./queries";
import { isPublicOrigin } from "./validation";

const schema = z.object({
  override: z
    .string()
    .trim()
    .max(2048, formError("ui.form.tooLong", { max: 2048 }))
    .refine(
      (value) => value === "" || isPublicOrigin(value),
      formError("settings.publicUrl.invalid"),
    ),
});

// Bound rather than literal: "//" in a template attribute confuses vue-tsc.
const PLACEHOLDER = "https://downloads.example.com";
const { t } = useI18n();
const settings = usePublicUrlSettings();
const save = useSavePublicUrl(settings.data);
const state = computed(() => settings.data.value);

function draftOf(value: PublicUrlState) {
  return { override: value.override_url ?? "" };
}

const { handleSubmit, resetForm, setFieldValue, meta, values } = useForm({
  validationSchema: zodSchema(schema),
});
useDirtyGuard(() => meta.value.dirty);

watch(
  state,
  (value) => {
    if (value && !meta.value.dirty) resetForm({ values: draftOf(value) });
  },
  { immediate: true },
);

const submit = handleSubmit(({ override }) => {
  save.mutate(override === "" ? null : override, {
    onSuccess: (value) => {
      resetForm({ values: draftOf(value) });
      toast({ tone: "success", title: t("settings.publicUrl.saved") });
    },
  });
});

async function reload() {
  await save.reload();
  if (state.value) resetForm({ values: draftOf(state.value) });
}

function discard() {
  if (state.value) resetForm({ values: draftOf(state.value) });
}
</script>

<template>
  <Card :title="t('settings.publicUrl.title')" :description="t('settings.publicUrl.description')">
    <AsyncState
      :loading="settings.isPending.value"
      :error="state ? undefined : settings.error.value"
      @retry="settings.refetch()"
    >
      <div v-if="state" class="flex flex-col gap-5">
        <dl class="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-[auto_1fr]">
          <dt class="text-muted">{{ t("settings.publicUrl.effective") }}</dt>
          <dd class="flex min-w-0 flex-wrap items-center gap-2">
            <code class="font-mono break-all">{{ state.effective_url }}</code>
            <Badge :tone="state.source === 'request' ? 'warning' : 'info'">
              {{ t(`settings.publicUrl.sources.${state.source}`) }}
            </Badge>
            <CopyButton :text="state.effective_url" />
          </dd>
          <dt class="text-muted">{{ t("settings.publicUrl.environment") }}</dt>
          <dd>
            <code v-if="state.environment_url" class="font-mono break-all">
              {{ state.environment_url }}
            </code>
            <span v-else class="text-muted">{{ t("settings.publicUrl.noEnvironment") }}</span>
          </dd>
        </dl>
        <p v-if="state.source === 'request'" class="text-sm text-muted">
          {{ t("settings.publicUrl.requestHint") }}
        </p>

        <form class="flex flex-col gap-4" novalidate @submit.prevent="submit">
          <RevisionConflictAlert v-if="save.hasConflict.value" @reload="reload" />
          <FormField
            v-slot="{ field }"
            name="override"
            :label="t('settings.publicUrl.override')"
            :description="t('settings.publicUrl.overrideHint')"
          >
            <Input
              v-bind="field"
              type="url"
              inputmode="url"
              autocomplete="off"
              spellcheck="false"
              :placeholder="PLACEHOLDER"
              :disabled="save.isPending.value"
            />
          </FormField>
          <div class="flex flex-wrap justify-end gap-2">
            <Button
              v-if="values.override"
              :disabled="save.isPending.value"
              @click="setFieldValue('override', '')"
            >
              {{ t("settings.publicUrl.clear") }}
            </Button>
            <Button v-if="meta.dirty" :disabled="save.isPending.value" @click="discard">
              {{ t("settings.discard") }}
            </Button>
            <Button
              type="submit"
              variant="primary"
              :loading="save.isPending.value"
              :disabled="!meta.dirty"
            >
              {{ t("settings.publicUrl.save") }}
            </Button>
          </div>
        </form>
      </div>
    </AsyncState>
  </Card>
</template>
