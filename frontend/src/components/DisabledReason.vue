<script setup lang="ts">
import { useId } from "vue";
// Disabled controls receive no pointer events, so the wrapper owns hover/focus and explains why.
defineProps<{ reason?: string }>();
const id = useId();
</script>
<template>
  <span
    v-if="reason"
    class="disabled-reason"
    tabindex="0"
    :aria-describedby="id"
    ><slot /><span :id="id" role="tooltip">{{ reason }}</span></span
  >
  <slot v-else />
</template>
