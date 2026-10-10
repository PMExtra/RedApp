<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";
import { useApp } from "@/features/directory";
import { NotesEditor } from "@/features/notes";

const route = useRoute();
const vendorId = computed(() => String(route.params.vendor));
const appId = computed(() => String(route.params.app));
const app = useApp(vendorId, appId);
</script>

<template>
  <NotesEditor
    v-if="app.data.value"
    :key="app.data.value.uid"
    :owner="{ vendor: vendorId, app: appId }"
    :read-only="app.data.value.deleted_at !== null"
  />
</template>
