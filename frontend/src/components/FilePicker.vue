<script setup lang="ts">
import { ref, useId } from "vue";
defineProps<{ name: string; label: string; accept?: string; disabled?: boolean; fileName?: string }>();
const emit = defineEmits<{ change: [Event] }>();
const input = ref<HTMLInputElement>();
const id = useId();
</script>
<template>
  <div class="file-picker">
    <input :id="id" ref="input" type="file" :name="name" :accept="accept" :disabled="disabled" :aria-label="label" hidden @change="emit('change', $event)" />
    <button type="button" class="secondary" :disabled="disabled" :aria-controls="id" @click="input?.click()">{{ label }}</button>
    <span v-if="fileName" class="muted small-text file-picker-name">{{ fileName }}</span>
  </div>
</template>
