<script setup lang="ts">
import { computed, useId } from "vue";
import { ArrowRight } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { VendorPicker } from "@/features/directory";
import { Alert, Badge, Checkbox, Field, Input, RadioGroup, type BadgeTone } from "@/shared/ui";
import {
  differenceText,
  needsProxy,
  offersDetach,
  offersNotes,
  type ChoiceDraft,
  type ProxyResolution,
} from "./choices";
import type { ImportItem } from "./queries";

/**
 * One item of an import preview with its decisions. Differences are shown as
 * plain text (instructions may contain HTML and scripts; never rendered).
 */
const draft = defineModel<ChoiceDraft>({ required: true });
const props = defineProps<{ item: ImportItem; renameable: boolean; disabled?: boolean }>();
const { t } = useI18n();
const id = useId();

function update(patch: Partial<ChoiceDraft>): void {
  draft.value = { ...draft.value, ...patch };
}

const actionTones: Record<ImportItem["action"], BadgeTone> = {
  create: "success",
  update: "info",
  skip: "neutral",
  keep: "neutral",
};

const actionOptions = computed(() => [
  { value: "skip", label: t("exchange.import.actions.skip") },
  props.item.revision > 0
    ? { value: "update", label: t("exchange.import.actions.update") }
    : { value: "create", label: t("exchange.import.actions.create") },
]);
const action = computed(() => draft.value.action ?? props.item.action);

const proxyOptions = computed(() => [
  ...(props.item.revision > 0
    ? [{ value: "keep_current", label: t("exchange.import.proxy.keepCurrent") }]
    : []),
  { value: "keep_effective", label: t("exchange.import.proxy.keepEffective") },
  { value: "inherit", label: t("exchange.import.proxy.inherit") },
  { value: "direct", label: t("exchange.import.proxy.direct") },
  { value: "url", label: t("exchange.import.proxy.url") },
]);

function setChecked(key: "dictionary_update" | "detach_template" | "update_notes") {
  return (value: boolean | "indeterminate") => {
    update({ [key]: value === true });
  };
}
</script>

<template>
  <section
    :aria-labelledby="`${id}-title`"
    class="flex flex-col gap-3 rounded-lg border border-border p-4"
  >
    <header class="flex flex-wrap items-center gap-2">
      <Badge>{{ t(`exchange.import.kinds.${item.kind}`) }}</Badge>
      <h3 :id="`${id}-title`" class="flex items-center gap-1.5 font-mono text-sm">
        <span>{{ item.key }}</span>
        <template v-if="item.target !== item.key">
          <ArrowRight class="size-3.5" aria-hidden="true" />
          <span class="sr-only">{{ t("exchange.import.renamedTo") }}</span>
          <span>{{ item.target }}</span>
        </template>
      </h3>
      <Badge :tone="actionTones[item.action]">
        {{ t(`exchange.import.actions.${item.action}`) }}
      </Badge>
    </header>

    <Alert v-if="item.template_hash_mismatch" tone="info">
      {{ t("exchange.import.templateChanged") }}
    </Alert>
    <Alert v-if="item.template_missing" tone="warning">
      {{ t("exchange.import.templateMissing") }}
    </Alert>

    <fieldset v-if="item.kind !== 'category'" class="flex flex-col gap-2" :disabled="disabled">
      <legend class="mb-1 text-sm font-medium">{{ t("exchange.import.actionLabel") }}</legend>
      <RadioGroup
        :model-value="action"
        :options="actionOptions"
        orientation="horizontal"
        :aria-label="t('exchange.import.actionFor', { key: item.key })"
        :disabled="disabled"
        @update:model-value="update({ action: $event as ChoiceDraft['action'] })"
      />
    </fieldset>
    <Checkbox
      v-else
      :model-value="draft.dictionary_update ?? false"
      :label="t('exchange.import.dictionaryUpdate')"
      :disabled="disabled"
      @update:model-value="setChecked('dictionary_update')($event)"
    />

    <div v-if="renameable && item.kind === 'app'" class="grid gap-3 sm:grid-cols-2">
      <Field v-slot="{ control }" :label="t('exchange.import.targetVendor')">
        <VendorPicker
          v-bind="control"
          :model-value="draft.target_vendor ?? ''"
          :placeholder="item.key.split('/')[0]"
          :disabled="disabled"
          @update:model-value="update({ target_vendor: $event, action: undefined })"
        />
      </Field>
      <Field v-slot="{ control }" :label="t('exchange.import.targetId')">
        <Input
          v-bind="control"
          :model-value="draft.target_id ?? ''"
          :placeholder="item.key.split('/')[1]"
          :disabled="disabled"
          autocomplete="off"
          spellcheck="false"
          maxlength="63"
          @update:model-value="update({ target_id: $event.trim(), action: undefined })"
        />
      </Field>
    </div>

    <Checkbox
      v-if="offersDetach(item)"
      :model-value="draft.detach_template ?? false"
      :label="t('exchange.import.detach')"
      :disabled="disabled"
      @update:model-value="setChecked('detach_template')($event)"
    />

    <fieldset v-if="needsProxy(item)" class="flex flex-col gap-2" :disabled="disabled">
      <legend class="mb-1 text-sm font-medium">{{ t("exchange.import.proxy.label") }}</legend>
      <p class="text-xs text-muted">{{ t("exchange.import.proxy.hint") }}</p>
      <RadioGroup
        :model-value="draft.proxy"
        :options="proxyOptions"
        :aria-label="t('exchange.import.proxy.label')"
        :disabled="disabled"
        @update:model-value="update({ proxy: $event as ProxyResolution })"
      />
      <Field
        v-if="draft.proxy === 'url'"
        v-slot="{ control }"
        :label="t('exchange.import.proxy.url')"
      >
        <Input
          v-bind="control"
          :model-value="draft.proxy_url ?? ''"
          :disabled="disabled"
          autocomplete="off"
          spellcheck="false"
          @update:model-value="update({ proxy_url: $event })"
        />
      </Field>
    </fieldset>

    <Checkbox
      v-if="offersNotes(item)"
      :model-value="draft.update_notes ?? false"
      :label="t('exchange.import.updateNotes')"
      :disabled="disabled"
      @update:model-value="setChecked('update_notes')($event)"
    />

    <ul v-if="item.requirements.length" class="flex flex-col gap-1">
      <li v-for="requirement in item.requirements" :key="requirement" class="text-sm text-warning">
        {{ t(`exchange.import.requirements.${requirement}`) }}
      </li>
    </ul>

    <div v-if="item.differences.length" class="flex flex-col gap-1">
      <p class="text-sm font-medium">
        {{
          t(
            "exchange.import.differences",
            { count: item.differences.length },
            item.differences.length,
          )
        }}
      </p>
      <details
        v-for="difference in item.differences"
        :key="difference.field"
        class="rounded-md border border-border"
      >
        <summary class="cursor-pointer px-3 py-1.5 font-mono text-xs focus-ring">
          {{ difference.field }}
        </summary>
        <div class="grid gap-2 p-3 md:grid-cols-2">
          <div>
            <p class="mb-1 text-xs text-muted">{{ t("exchange.import.before") }}</p>
            <pre
              class="max-h-60 overflow-auto rounded-md bg-surface-sunken p-2 font-mono text-xs whitespace-pre-wrap"
              >{{ differenceText(difference.before) }}</pre>
          </div>
          <div>
            <p class="mb-1 text-xs text-muted">{{ t("exchange.import.after") }}</p>
            <pre
              class="max-h-60 overflow-auto rounded-md bg-surface-sunken p-2 font-mono text-xs whitespace-pre-wrap"
              >{{ differenceText(difference.after) }}</pre>
          </div>
        </div>
      </details>
    </div>
  </section>
</template>
