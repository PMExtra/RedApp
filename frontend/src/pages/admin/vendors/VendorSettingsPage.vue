<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";
import { DeleteSection, useVendor, VendorSettingsForm } from "@/features/directory";

// The layout loads the vendor (shared query cache) and shows loading/errors.
const route = useRoute();
const vendor = useVendor(computed(() => String(route.params.vendor)));
</script>

<template>
  <div v-if="vendor.data.value" class="flex flex-col gap-6">
    <VendorSettingsForm :key="vendor.data.value.uid" :vendor="vendor.data.value" />
    <DeleteSection v-if="!vendor.data.value.deleted_at" :entity="vendor.data.value" />
  </div>
</template>
