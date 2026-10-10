<script setup lang="ts">
import { computed, useId } from "vue";
import { useI18n } from "vue-i18n";
import type { Schema } from "@/shared/api";
import { Field, Input, RadioGroup, type RadioOption } from "@/shared/ui";

export type ProxyValue = Schema<"ProxyConfig">;

/**
 * Proxy mode and URL for the global setting (`allowInherit: false`) and for
 * vendors/applications (inherit, direct or URL). A saved password comes back
 * as `****`; sending it unchanged keeps the stored password.
 */
const model = defineModel<ProxyValue>({ required: true });
const props = defineProps<{
  allowInherit?: boolean;
  /** Resolved proxy after inheritance, shown below the fields. */
  effective?: Schema<"ProxyEffective">;
  disabled?: boolean;
  urlError?: string;
}>();
const { t } = useI18n();
const id = useId();
const URL_PLACEHOLDER = "http://user:password@proxy.example:3128";

const options = computed<RadioOption[]>(() => [
  ...(props.allowInherit ? [{ value: "inherit", label: t("proxy.modes.inherit") }] : []),
  { value: "direct", label: t("proxy.modes.direct") },
  { value: "url", label: t("proxy.modes.url") },
]);

const mode = computed({
  get: () => model.value.mode,
  set: (value: string | undefined) => {
    if (value === "url") model.value = { mode: "url", url: model.value.url ?? "" };
    else if (value === "direct" || value === "inherit") model.value = { mode: value };
  },
});
const url = computed({
  get: () => model.value.url ?? "",
  set: (value: string) => {
    model.value = { mode: "url", url: value };
  },
});
const effectiveText = computed(() => {
  const value = props.effective;
  if (!value) return "";
  const target = value.mode === "url" ? (value.url ?? "") : t("proxy.modes.direct");
  return t("proxy.effective", { target, scope: t(`proxy.scopes.${value.source_scope}`) });
});
</script>

<template>
  <fieldset class="flex flex-col gap-3" :disabled="disabled">
    <legend class="mb-2 text-sm font-medium">{{ t("proxy.legend") }}</legend>
    <RadioGroup v-model="mode" :options="options" orientation="horizontal" :name="`${id}-mode`" />
    <template v-if="mode === 'url'">
      <Field
        v-slot="{ control }"
        :label="t('proxy.url')"
        :description="t('proxy.urlHint')"
        :error="urlError"
        required
      >
        <!-- A literal "…@host:port" placeholder attribute confuses vue-tsc; bind it. -->
        <Input
          v-bind="control"
          v-model="url"
          type="text"
          inputmode="url"
          autocomplete="off"
          spellcheck="false"
          maxlength="4096"
          :placeholder="URL_PLACEHOLDER"
        />
      </Field>
    </template>
    <p v-if="effectiveText" class="text-xs text-muted">{{ effectiveText }}</p>
  </fieldset>
</template>
