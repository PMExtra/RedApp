<script setup lang="ts">
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";
import { useAppConfiguration } from "@/features/configuration";
import { appRoute, VendorPicker, type App } from "@/features/directory";
import { useNotes } from "@/features/notes";
import { isApiError, type Schema } from "@/shared/api";
import { isSlug, isVendorId, toast } from "@/shared/lib";
import {
  Alert,
  Button,
  Checkbox,
  Dialog,
  Field,
  Input,
  RadioGroup,
  RevisionConflictAlert,
} from "@/shared/ui";
import { useCopyApp, type ExchangeMode } from "./queries";

/**
 * Creates a new, disabled application from `app` (no cache, files, history
 * or tasks). Notes are copied only on request, guarded by their revision.
 */
const open = defineModel<boolean>("open", { default: false });
const props = defineProps<{ app: App }>();
const { t } = useI18n();
const router = useRouter();

const configuration = useAppConfiguration(
  () => props.app.vendor_id,
  () => props.app.id,
);
const copy = useCopyApp(() => props.app);
const owner = computed(() => ({ vendor: props.app.vendor_id, app: props.app.id }));
const notes = useNotes(owner, false);

// The dialog is mounted per opening, so every copy starts from these defaults.
const targetVendor = ref(props.app.vendor_id);
const targetId = ref("");
const chosenMode = ref<string | undefined>();
const includeNotes = ref<boolean | "indeterminate">(false);
const submitted = ref(false);
const linked = computed(() => configuration.data.value?.template_ref != null);
// `linked` keeps the template reference and is only possible with a template.
const mode = computed({
  get: () => (linked.value ? (chosenMode.value ?? "linked") : "independent"),
  set: (value: string | undefined) => {
    chosenMode.value = value;
  },
});

const modes = computed(() => [
  {
    value: "linked",
    label: t("exchange.modes.linked"),
    description: t("exchange.modes.linkedHint"),
    disabled: !linked.value,
  },
  {
    value: "independent",
    label: t("exchange.modes.independent"),
    description: t("exchange.modes.independentHint"),
  },
]);

const vendorError = computed(() =>
  submitted.value && !isVendorId(targetVendor.value) ? t("exchange.copy.vendorInvalid") : undefined,
);
const idError = computed(() =>
  submitted.value && !isSlug(targetId.value) ? t("exchange.copy.idInvalid") : undefined,
);

async function submit(): Promise<void> {
  submitted.value = true;
  if (vendorError.value || idError.value) return;
  const body: Schema<"CopyRequest"> = {
    source_uid: props.app.uid,
    target_vendor: targetVendor.value,
    target_id: targetId.value,
    mode: mode.value as ExchangeMode,
    include_notes: includeNotes.value === true,
  };
  try {
    if (body.include_notes) {
      // Guard the copied notes by their current revision.
      const current = await notes.refetch({ throwOnError: true });
      if (current.data) body.notes_revision = current.data.revision;
    }
    const created = await copy.mutateAsync(body);
    toast({ tone: "success", title: t("exchange.copy.done", { key: created.key }) });
    open.value = false;
    await router.push(appRoute(created, "settings"));
  } catch (error) {
    if (isApiError(error, "ALREADY_EXISTS")) {
      toast({ tone: "error", title: t("exchange.copy.exists") });
    }
  }
}
</script>

<template>
  <Dialog
    v-model:open="open"
    :title="t('exchange.copy.title', { key: app.key })"
    :description="t('exchange.copy.description')"
    :persistent="copy.isPending.value"
  >
    <form id="app-copy" class="flex flex-col gap-4" novalidate @submit.prevent="submit">
      <RevisionConflictAlert v-if="copy.hasConflict.value" @reload="copy.reloadSource()" />
      <div class="grid gap-4 sm:grid-cols-2">
        <Field
          v-slot="{ control }"
          :label="t('exchange.copy.targetVendor')"
          :error="vendorError"
          required
        >
          <VendorPicker v-bind="control" v-model="targetVendor" />
        </Field>
        <Field
          v-slot="{ control }"
          :label="t('exchange.copy.targetId')"
          :description="t('exchange.copy.idHint')"
          :error="idError"
          required
        >
          <Input
            v-bind="control"
            v-model="targetId"
            autocomplete="off"
            autocapitalize="none"
            spellcheck="false"
            maxlength="63"
            class="font-mono"
          />
        </Field>
      </div>
      <fieldset class="flex flex-col gap-2">
        <legend class="mb-1 text-sm font-medium">{{ t("exchange.modes.label") }}</legend>
        <RadioGroup v-model="mode" :options="modes" :aria-label="t('exchange.modes.label')" />
      </fieldset>
      <Checkbox v-model="includeNotes" :label="t('exchange.copy.includeNotes')" />
      <Alert tone="info">{{ t("exchange.copy.notice") }}</Alert>
    </form>
    <template #footer>
      <Button :disabled="copy.isPending.value" @click="open = false">
        {{ t("common.actions.cancel") }}
      </Button>
      <Button type="submit" form="app-copy" variant="primary" :loading="copy.isPending.value">
        {{ t("exchange.copy.submit") }}
      </Button>
    </template>
  </Dialog>
</template>
