<script setup lang="ts">
defineProps<{
  modelValue: boolean;
  label: string;
  disabled?: boolean;
  name?: string;
}>();
const emit = defineEmits<{ "update:modelValue": [boolean] }>();
</script>
<template>
  <button
    type="button"
    class="switch-control"
    role="switch"
    :name="name"
    :aria-label="label"
    :aria-checked="modelValue"
    :disabled="disabled"
    @click="emit('update:modelValue', !modelValue)"
  >
    <span class="switch-track" aria-hidden="true"><span /></span
    ><span>{{ label }}</span>
  </button>
</template>
