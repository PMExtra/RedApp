<script setup lang="ts">
withDefaults(defineProps<{ as?: "ul" | "div" }>(), { as: "div" });
</script>
<template>
  <component :is="as" class="popover-panel"><slot /></component>
</template>
