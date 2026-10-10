<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";
import { AppsTable, useVendor } from "@/features/directory";

const route = useRoute();
const vendor = useVendor(computed(() => String(route.params.vendor)));
</script>

<template>
  <AppsTable v-if="vendor.data.value" :key="vendor.data.value.uid" :vendor="vendor.data.value" />
</template>
