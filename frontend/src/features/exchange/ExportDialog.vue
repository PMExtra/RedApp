<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Download } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { toast } from "@/shared/lib";
import { Alert, Button, Checkbox, Dialog, RadioGroup } from "@/shared/ui";
import { useExport, type ExchangeMode } from "./queries";

/**
 * Exports a vendor (optionally with its applications) or one application as
 * a configuration ZIP. Sensitive options are off every time it opens.
 */
const open = defineModel<boolean>("open", { default: false });
const props = defineProps<{ kind: "vendor" | "app"; entityKey: string }>();
const { t } = useI18n();
const exporter = useExport();

// Checkbox and RadioGroup models are wider than the values they produce here.
const includeApps = ref<boolean | "indeterminate">(true);
const mode = ref<string | undefined>("linked");
const includeNotes = ref<boolean | "indeterminate">(false);
const includeCredentials = ref<boolean | "indeterminate">(false);

watch(open, (value) => {
  if (!value) return;
  includeApps.value = true;
  mode.value = "linked";
  includeNotes.value = false;
  includeCredentials.value = false;
});

const modes = computed(() => [
  {
    value: "linked",
    label: t("exchange.modes.linked"),
    description: t("exchange.modes.linkedHint"),
  },
  {
    value: "independent",
    label: t("exchange.modes.independent"),
    description: t("exchange.modes.independentHint"),
  },
]);

async function submit(): Promise<void> {
  try {
    await exporter.mutateAsync({
      selection: [
        props.kind === "vendor"
          ? { kind: "vendor", key: props.entityKey, include_apps: includeApps.value === true }
          : { kind: "app", key: props.entityKey },
      ],
      mode: (mode.value ?? "linked") as ExchangeMode,
      include_notes: includeNotes.value === true,
      include_proxy_credentials: includeCredentials.value === true,
    });
    toast({ tone: "success", title: t("exchange.export.done") });
    open.value = false;
  } catch {
    // Reported by the global error handler.
  }
}
</script>

<template>
  <Dialog
    v-model:open="open"
    :title="t('exchange.export.title', { key: entityKey })"
    :description="t('exchange.export.description')"
    :persistent="exporter.isPending.value"
  >
    <form id="configuration-export" class="flex flex-col gap-5" @submit.prevent="submit">
      <Checkbox
        v-if="kind === 'vendor'"
        v-model="includeApps"
        :label="t('exchange.export.includeApps')"
      />
      <fieldset class="flex flex-col gap-2">
        <legend class="mb-1 text-sm font-medium">{{ t("exchange.modes.label") }}</legend>
        <RadioGroup v-model="mode" :options="modes" :aria-label="t('exchange.modes.label')" />
      </fieldset>
      <fieldset class="flex flex-col gap-2">
        <legend class="mb-1 text-sm font-medium">{{ t("exchange.export.sensitive") }}</legend>
        <Checkbox v-model="includeNotes" :label="t('exchange.export.includeNotes')" />
        <Checkbox v-model="includeCredentials" :label="t('exchange.export.includeCredentials')" />
      </fieldset>
      <Alert v-if="includeNotes || includeCredentials" tone="warning">
        {{ t("exchange.export.sensitiveWarning") }}
      </Alert>
      <p class="text-xs text-muted">{{ t("exchange.export.scope") }}</p>
    </form>
    <template #footer>
      <Button :disabled="exporter.isPending.value" @click="open = false">
        {{ t("common.actions.cancel") }}
      </Button>
      <Button
        type="submit"
        form="configuration-export"
        variant="primary"
        :loading="exporter.isPending.value"
      >
        <Download aria-hidden="true" /> {{ t("exchange.export.submit") }}
      </Button>
    </template>
  </Dialog>
</template>
