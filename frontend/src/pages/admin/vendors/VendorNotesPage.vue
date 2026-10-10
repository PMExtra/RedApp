<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";
import { useVendor } from "@/features/directory";
import { NotesEditor } from "@/features/notes";

const route = useRoute();
const vendorId = computed(() => String(route.params.vendor));
const vendor = useVendor(vendorId);
</script>

<template>
  <NotesEditor
    v-if="vendor.data.value"
    :key="vendor.data.value.uid"
    :owner="{ vendor: vendorId }"
    :read-only="vendor.data.value.deleted_at !== null"
  />
</template>
