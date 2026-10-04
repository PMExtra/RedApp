<script setup lang="ts">
import SwitchControl from "./SwitchControl.vue";
import { t } from "../i18n";
defineProps<{ modelValue: boolean; disabled?: boolean }>();
defineEmits<{ "update:modelValue": [boolean] }>();
</script>
<template>
  <SwitchControl
    class="auto-refresh"
    :model-value="modelValue"
    :disabled="disabled"
    :label="t('Auto refresh')"
    :title="t('Refresh every 5 seconds')"
    @update:model-value="$emit('update:modelValue', $event)"
  />
</template>
