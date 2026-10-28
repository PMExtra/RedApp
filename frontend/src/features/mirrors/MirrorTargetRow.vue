<script setup lang="ts">
import { computed, watch } from "vue";
import { CircleCheck } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import { appRoute, type App } from "@/features/directory";
import { describeError } from "@/shared/api";
import { useLocalized } from "@/shared/i18n";
import { isSlug, isVendorId, useDebounced } from "@/shared/lib";
import { Alert, Button, Checkbox, Field, Input, Spinner } from "@/shared/ui";
import { useLocalApp, type UpstreamCatalogItem } from "./queries";
import { localProvider, type TargetStatus } from "./target";

/**
 * One application of the mirror dialog: the local vendor and application IDs
 * (checked against the local directory as they are typed) and, when that
 * application exists, the explicit choice to adopt it. Reports its status to
 * the dialog, which decides whether the row can be submitted.
 */
const vendor = defineModel<string>("vendor", { required: true });
const app = defineModel<string>("app", { required: true });
const adopt = defineModel<boolean>("adopt", { required: true });
const props = defineProps<{
  remote: UpstreamCatalogItem;
  /** Provider names by key. */
  providerName: (key: string) => string;
  /** Mirrored application, once done. */
  done?: App;
  /** Error message of the last attempt. */
  error?: string;
  /** Show validation messages (after a submit attempt). */
  submitted: boolean;
  disabled: boolean;
}>();
const emit = defineEmits<{ status: [status: TargetStatus] }>();
const { t } = useI18n();
const localized = useLocalized();

const LOOKUP_DELAY_MS = 300;
const typedVendor = computed(() => vendor.value.trim());
const typedApp = computed(() => app.value.trim());
const lookupVendor = useDebounced(typedVendor, LOOKUP_DELAY_MS);
const lookupApp = useDebounced(typedApp, LOOKUP_DELAY_MS);
const valid = computed(() => isVendorId(typedVendor.value) && isSlug(typedApp.value));
const settled = computed(
  () => lookupVendor.value === typedVendor.value && lookupApp.value === typedApp.value,
);
const local = useLocalApp(
  lookupVendor,
  lookupApp,
  () => props.done === undefined && isVendorId(lookupVendor.value) && isSlug(lookupApp.value),
);

// Adopting is confirmed for one target: another ID needs a new confirmation.
watch([typedVendor, typedApp], () => {
  adopt.value = false;
});

const status = computed<TargetStatus>(() => {
  if (!valid.value) return { kind: "invalid" };
  // Typing, or (re)reading the target: the row cannot be submitted yet.
  if (!settled.value || local.isFetching.value) return { kind: "checking" };
  if (local.error.value && local.data.value === undefined) {
    return { kind: "error", message: describeError(local.error.value).message };
  }
  const existing = local.data.value;
  if (existing === undefined) return { kind: "checking" };
  if (existing === null) return { kind: "new" };
  if (existing.deleted_at) return { kind: "deleted", existing };
  if (existing.provider !== localProvider(props.remote.provider)) {
    return { kind: "mismatch", existing };
  }
  return { kind: "existing", existing };
});
watch(status, (value) => emit("status", value), { immediate: true });

const key = computed(() => `${typedVendor.value}/${typedApp.value}`);
const name = computed(() => localized(props.remote.name) || props.remote.key);
const vendorError = computed(() =>
  props.submitted && !isVendorId(typedVendor.value) ? t("mirrors.mirror.vendorInvalid") : undefined,
);
const appError = computed(() =>
  props.submitted && !isSlug(typedApp.value) ? t("mirrors.mirror.appInvalid") : undefined,
);
</script>

<template>
  <li class="flex flex-col gap-3 rounded-lg border border-border p-4">
    <div class="flex flex-wrap items-baseline justify-between gap-2">
      <span class="font-medium">{{ name }}</span>
      <span class="text-xs text-muted">
        <span class="font-mono">{{ remote.key }}</span> · {{ providerName(remote.provider) }}
      </span>
    </div>

    <p v-if="done" class="flex items-center gap-2 text-sm text-success" role="status">
      <CircleCheck class="size-4" aria-hidden="true" />
      <RouterLink
        :to="appRoute(done, 'settings')"
        class="rounded-sm font-medium underline focus-ring"
      >
        {{ t("mirrors.mirror.done", { key: done.key }) }}
      </RouterLink>
    </p>

    <template v-else>
      <fieldset :disabled="disabled" class="grid gap-3 sm:grid-cols-2">
        <legend class="sr-only">{{ t("mirrors.mirror.target", { key: remote.key }) }}</legend>
        <Field v-slot="{ control }" :label="t('mirrors.mirror.vendor')" :error="vendorError">
          <Input
            v-bind="control"
            v-model="vendor"
            autocomplete="off"
            autocapitalize="none"
            spellcheck="false"
            maxlength="63"
            class="font-mono"
          />
        </Field>
        <Field v-slot="{ control }" :label="t('mirrors.mirror.app')" :error="appError">
          <Input
            v-bind="control"
            v-model="app"
            autocomplete="off"
            autocapitalize="none"
            spellcheck="false"
            maxlength="63"
            class="font-mono"
          />
        </Field>
      </fieldset>

      <!-- Announces settled results only: nothing while typing, "checking" while reading. -->
      <div class="text-sm" aria-live="polite">
        <template v-if="status.kind === 'checking'">
          <p v-if="local.isFetching.value" class="flex items-center gap-2 text-muted">
            <Spinner size="sm" /> {{ t("mirrors.mirror.checking") }}
          </p>
        </template>
        <p v-else-if="status.kind === 'new'" class="text-muted">
          {{ t("mirrors.mirror.isNew") }}
        </p>
        <div v-else-if="status.kind === 'error'" class="flex flex-wrap items-center gap-2">
          <p class="text-danger">{{ status.message }}</p>
          <Button size="sm" :disabled="disabled" @click="local.refetch()">
            {{ t("mirrors.mirror.recheck") }}
          </Button>
        </div>
        <p v-else-if="status.kind === 'deleted'" class="text-danger">
          {{ t("mirrors.mirror.deletedTarget") }}
        </p>
        <p v-else-if="status.kind === 'mismatch'" class="text-danger">
          {{
            t("mirrors.mirror.providerMismatch", {
              key,
              local: providerName(status.existing.provider),
              remote: providerName(remote.provider),
              expected: providerName(localProvider(remote.provider)),
            })
          }}
        </p>
        <div v-else-if="status.kind === 'existing'" class="flex flex-col gap-1">
          <p>{{ t("mirrors.mirror.exists", { key }) }}</p>
          <Checkbox v-model="adopt" :disabled="disabled" :label="t('mirrors.mirror.adopt')" />
          <p class="text-xs text-muted">{{ t("mirrors.mirror.adoptHint") }}</p>
          <p v-if="submitted && !adopt" class="text-danger">
            {{ t("mirrors.mirror.adoptRequired") }}
          </p>
        </div>
      </div>

      <Alert v-if="error" tone="danger" :title="t('mirrors.mirror.failed')">{{ error }}</Alert>
    </template>
  </li>
</template>
