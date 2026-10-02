<script setup lang="ts">
import { onMounted, onUnmounted, watch } from "vue";
import { useRoute } from "vue-router";
import { loadBootstrap, invalidateBootstrap } from "./bootstrap";
const route = useRoute();
void loadBootstrap();
const visible = () => {
  if (document.visibilityState !== "hidden") void loadBootstrap();
};
watch(
  () => route.path,
  () => void loadBootstrap(),
);
onMounted(() => {
  document.addEventListener("visibilitychange", visible);
});
onUnmounted(() => {
  invalidateBootstrap();
  document.removeEventListener("visibilitychange", visible);
});
</script>
<template><RouterView /></template>
