<script setup lang="ts">
import Icon from "./Icon.vue";
withDefaults(
  defineProps<{
    icon: string;
    label: string;
    type?: "button" | "submit" | "reset";
  }>(),
  { type: "button" },
);
</script>
<template>
  <button :type="type" class="icon-button" :aria-label="label" :title="label">
    <Icon :name="icon" />
  </button>
</template>
