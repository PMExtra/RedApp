<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";
import Maintenance from "../components/Maintenance.vue";
const route = useRoute();
const application = computed(
  () => `${route.params.vendor}/${route.params.app}`,
);
</script>
<template><Maintenance :application="application" /></template>
