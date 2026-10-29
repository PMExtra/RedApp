<script setup lang="ts">
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { RedirectProtectionFields } from "@/features/proxy";
import type { Schema } from "@/shared/api";
import { toast } from "@/shared/lib";
import { AsyncState, Button, Card, RevisionConflictAlert } from "@/shared/ui";
import {
  useRedirectProtectionSettings,
  useSaveRedirectProtection,
  type RedirectProtectionState,
} from "./queries";
import { useServerDraft } from "./serverDraft";

type RedirectProtectionMode = Schema<"RedirectProtectionMode">;

const { t } = useI18n();
const settings = useRedirectProtectionSettings();
const save = useSaveRedirectProtection(settings.data);
const state = computed(() => settings.data.value);

// The saved mode the draft was made from; `null` until loaded.
const base = ref<RedirectProtectionMode | null>(null);
const draft = ref<RedirectProtectionMode>("strict");
const dirty = computed(() => base.value !== null && draft.value !== base.value);
const baseline = useServerDraft({
  state: () => state.value,
  dirty: () => dirty.value,
  adopt: (value: RedirectProtectionState) => {
    base.value = value.mode;
    draft.value = value.mode;
  },
});

function submit() {
  if (draft.value === "inherit") return;
  save.mutate(
    { mode: draft.value },
    {
      onSuccess: (value) => {
        baseline.reset(value);
        toast({ tone: "success", title: t("settings.redirectProtection.saved") });
      },
    },
  );
}

async function reload() {
  await save.reload();
  baseline.reset();
}
</script>

<template>
  <Card
    :title="t('settings.redirectProtection.title')"
    :description="t('settings.redirectProtection.description')"
  >
    <AsyncState
      :loading="settings.isPending.value"
      :error="state ? undefined : settings.error.value"
      @retry="settings.refetch()"
    >
      <form v-if="state" class="flex flex-col gap-5" novalidate @submit.prevent="submit">
        <RevisionConflictAlert v-if="save.hasConflict.value" @reload="reload" />
        <RedirectProtectionFields v-model="draft" :disabled="save.isPending.value" />
        <div class="flex flex-wrap justify-end gap-2">
          <Button v-if="dirty" :disabled="save.isPending.value" @click="baseline.reset()">
            {{ t("settings.discard") }}
          </Button>
          <Button
            type="submit"
            variant="primary"
            :loading="save.isPending.value"
            :disabled="!dirty"
          >
            {{ t("settings.redirectProtection.save") }}
          </Button>
        </div>
      </form>
    </AsyncState>
  </Card>
</template>
