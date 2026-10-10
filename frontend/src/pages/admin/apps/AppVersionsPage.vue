<script setup lang="ts">
// Versions tab of release applications: versions and stored files.
// AppLayout only shows it for codex and claude-code applications that are not deleted.
import { computed } from "vue";
import { useRoute } from "vue-router";
import { ReleaseInventory } from "@/features/releases";
import { useAutoRefresh } from "@/shared/lib";
import { AutoRefreshToggle } from "@/shared/ui";

const route = useRoute();
const vendor = computed(() => String(route.params.vendor));
const app = computed(() => String(route.params.app));
const auto = useAutoRefresh();
</script>

<template>
  <div class="flex flex-col gap-6">
    <div class="flex justify-end">
      <AutoRefreshToggle v-model="auto.enabled.value" />
    </div>
    <ReleaseInventory :vendor="vendor" :app="app" :auto-refresh="auto.enabled.value" />
  </div>
</template>
