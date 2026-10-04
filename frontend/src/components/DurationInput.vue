<script setup lang="ts">
import { computed, ref } from "vue";
import { t } from "../i18n";
const props = defineProps<{ modelValue: number }>();
const emit = defineEmits<{ 'update:modelValue': [number] }>();
const unit = ref(props.modelValue % 86400 === 0 ? 86400 : props.modelValue % 3600 === 0 ? 3600 : props.modelValue % 60 === 0 ? 60 : 1);
const amount = computed(() => props.modelValue / unit.value);
</script>
<template>
  <div class="duration-input two-columns">
    <label>{{ t('Age to keep') }}<input name="cleanup_age" type="number" :value="amount" :min="60 / unit" :max="315360000 / unit" step="any" required @input="emit('update:modelValue', Math.round(Number(($event.target as HTMLInputElement).value) * unit))" /></label>
    <label>{{ t('Time unit') }}<select v-model.number="unit" name="cleanup_unit"><option :value="1">{{ t('Seconds') }}</option><option :value="60">{{ t('Minutes') }}</option><option :value="3600">{{ t('Hours') }}</option><option :value="86400">{{ t('Days') }}</option></select></label>
  </div>
</template>
