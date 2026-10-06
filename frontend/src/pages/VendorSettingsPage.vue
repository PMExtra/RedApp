<script setup lang="ts">
import DirectoryEditor from "../components/DirectoryEditor.vue";
import type { Vendor } from "../directory";
defineProps<{ vendor: Vendor }>();
const emit = defineEmits<{ updated: [Vendor] }>();
</script>
<template>
  <DirectoryEditor :key="vendor.uid" kind="vendor" @updated="emit('updated', $event)" />
</template>
