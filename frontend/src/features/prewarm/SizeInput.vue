<script setup lang="ts">
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { Field, NumberInput, Select, type SelectOption } from "@/shared/ui";

/** A byte count edited as an amount of MiB, GiB or TiB. */
const model = defineModel<number>({ required: true });
const props = defineProps<{
  label: string;
  description?: string;
  max: number;
  disabled?: boolean;
}>();
const { t } = useI18n();
const UNITS = [1024 ** 4, 1024 ** 3, 1024 ** 2] as const;
const NAMES: Record<number, string> = {
  [1024 ** 4]: "TiB",
  [1024 ** 3]: "GiB",
  [1024 ** 2]: "MiB",
};

const unit = ref<number>(UNITS.find((size) => model.value % size === 0) ?? 1024 ** 2);
const options = computed<SelectOption[]>(() =>
  [...UNITS].reverse().map((size) => ({ value: String(size), label: NAMES[size] ?? "" })),
);
const unitValue = computed({
  get: () => String(unit.value),
  set: (value: string | undefined) => {
    const next = Number(value);
    if (!next) return;
    const amount = model.value / unit.value;
    unit.value = next;
    model.value = Math.min(props.max, Math.max(1, Math.round(amount * next)));
  },
});
const amount = computed({
  get: () => Math.round((model.value / unit.value) * 100) / 100,
  set: (value: number | null) => {
    if (value !== null)
      model.value = Math.min(props.max, Math.max(1, Math.round(value * unit.value)));
  },
});
</script>

<template>
  <Field v-slot="{ control }" :label="label" :description="description">
    <div class="flex flex-wrap items-center gap-2">
      <div class="w-36">
        <NumberInput
          v-bind="control"
          v-model="amount"
          :min="0.01"
          :max="max / unit"
          :step="1"
          :disabled="disabled"
        />
      </div>
      <Select
        v-model="unitValue"
        class="w-28"
        :options="options"
        :disabled="disabled"
        :aria-label="t('prewarm.limits.sizeUnit', { label })"
      />
    </div>
  </Field>
</template>
