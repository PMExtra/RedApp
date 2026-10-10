<script setup lang="ts">
// Files tab of hosted applications (AppLayout only shows it for them).
import { computed } from "vue";
import { useRoute } from "vue-router";
import { useApp } from "@/features/directory";
import { HostedFilesPanel } from "@/features/hosted";
import { AsyncState } from "@/shared/ui";

const route = useRoute();
const vendor = computed(() => String(route.params.vendor));
const app = computed(() => String(route.params.app));
const record = useApp(vendor, app);
const deleted = computed(() => Boolean(record.data.value?.deleted_at));
</script>

<template>
  <AsyncState
    :loading="record.isPending.value"
    :error="record.error.value"
    @retry="record.refetch()"
  >
    <HostedFilesPanel v-if="record.data.value" :vendor="vendor" :app="app" :deleted="deleted" />
  </AsyncState>
</template>
