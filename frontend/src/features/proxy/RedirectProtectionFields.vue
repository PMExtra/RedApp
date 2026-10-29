<script setup lang="ts">
import { computed, useId } from "vue";
import { useI18n } from "vue-i18n";
import type { Schema } from "@/shared/api";
import { RadioGroup, type RadioOption } from "@/shared/ui";

export type RedirectProtectionMode = Schema<"RedirectProtectionMode">;

/**
 * Redirect protection for the global setting (`allowInherit: false`) and for
 * vendors/applications (inherit, strict, basic or off). The hint explains the
 * selected mode, or the inherited one.
 */
const model = defineModel<RedirectProtectionMode>({ required: true });
const props = defineProps<{
  allowInherit?: boolean;
  /** Resolved protection after inheritance, shown below the fields. */
  effective?: Schema<"RedirectProtectionEffective">;
  disabled?: boolean;
}>();
defineSlots<{ /** At the end of the legend row (FieldReset). */ reset?: () => unknown }>();
const { t } = useI18n();
const id = useId();

const options = computed<RadioOption[]>(() => [
  ...(props.allowInherit
    ? [{ value: "inherit", label: t("redirectProtection.modes.inherit") }]
    : []),
  ...(["strict", "basic", "off"] as const).map((value) => ({
    value,
    label: t(`redirectProtection.modes.${value}`),
  })),
]);

const mode = computed({
  get: () => model.value,
  set: (value: string | undefined) => {
    if (value === "inherit" || value === "strict" || value === "basic" || value === "off") {
      model.value = value;
    }
  },
});
const hint = computed(() => {
  const value = model.value === "inherit" ? props.effective?.mode : model.value;
  return value ? t(`redirectProtection.hints.${value}`) : "";
});
const effectiveText = computed(() => {
  const value = props.effective;
  if (!value) return "";
  return t("redirectProtection.effective", {
    mode: t(`redirectProtection.modes.${value.mode}`),
    scope: t(`proxy.scopes.${value.source_scope}`),
  });
});
</script>

<template>
  <!-- The legend sits in a row with the reset slot, so it names the group explicitly. -->
  <fieldset class="flex flex-col gap-3" :disabled="disabled" :aria-labelledby="`${id}-legend`">
    <div class="mb-2 flex h-6 items-center justify-between gap-2">
      <legend :id="`${id}-legend`" class="text-sm font-medium">
        {{ t("redirectProtection.legend") }}
      </legend>
      <slot name="reset" />
    </div>
    <RadioGroup v-model="mode" :options="options" orientation="horizontal" :name="`${id}-mode`" />
    <p v-if="hint" class="text-xs text-muted">{{ hint }}</p>
    <p v-if="effectiveText" class="text-xs text-muted">{{ effectiveText }}</p>
  </fieldset>
</template>
