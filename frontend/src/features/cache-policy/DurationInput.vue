<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { Field, NumberInput, Select, type SelectOption } from "@/shared/ui";

/**
 * Seconds edited as an amount and a unit (seconds, minutes, hours, days).
 * Clearing the amount keeps the last value.
 */
const model = defineModel<number>({ required: true });
const props = withDefaults(
  defineProps<{
    label: string;
    description?: string;
    error?: string;
    /** Bounds in seconds. */
    min?: number;
    max?: number;
    disabled?: boolean;
    required?: boolean;
  }>(),
  { min: 0, max: 315_360_000 },
);
const { t } = useI18n();
const UNITS = [86_400, 3_600, 60, 1] as const;

function unitFor(seconds: number): number {
  if (!seconds) return props.min >= 86_400 ? 86_400 : props.min >= 60 ? 60 : 1;
  return UNITS.find((size) => seconds % size === 0) ?? 1;
}

const unit = ref(unitFor(model.value));
watch(model, (seconds) => {
  // Keep the chosen unit while it still divides the value (e.g. while typing).
  if (seconds % unit.value !== 0) unit.value = unitFor(seconds);
});

const unitOptions = computed<SelectOption[]>(() =>
  UNITS.filter((size) => size <= props.max)
    .map((size) => ({ value: String(size), label: t(`cachePolicy.duration.units.${size}`) }))
    .reverse(),
);
const unitValue = computed({
  get: () => String(unit.value),
  set: (value: string | undefined) => {
    const next = Number(value);
    if (!next) return;
    const amount = model.value / unit.value;
    unit.value = next;
    model.value = Math.round(amount * next);
  },
});
const amount = computed({
  get: () => model.value / unit.value,
  set: (value: number | null) => {
    if (value !== null) model.value = Math.round(value * unit.value);
  },
});
</script>

<template>
  <Field
    v-slot="{ control }"
    :label="label"
    :description="description"
    :error="error"
    :required="required"
  >
    <div class="flex flex-wrap items-center gap-2">
      <div class="w-36">
        <NumberInput
          v-bind="control"
          v-model="amount"
          :min="Math.ceil(min / unit)"
          :max="Math.floor(max / unit)"
          :step="1"
          :disabled="disabled"
        />
      </div>
      <Select
        v-model="unitValue"
        class="w-32"
        :options="unitOptions"
        :disabled="disabled"
        :aria-label="t('cachePolicy.duration.unit', { label })"
      />
    </div>
  </Field>
</template>
