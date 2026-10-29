<script setup lang="ts">
// Versions tab of release applications; each version links to AppVersionPage.
// AppLayout only shows it for codex, claude-code and json-release applications
// that are not deleted. A JSON release with a lookup can discover a version.
import { computed } from "vue";
import { useRoute } from "vue-router";
import { useApp } from "@/features/directory";
import { ReleaseLookupForm } from "@/features/json-release";
import { VersionList } from "@/features/releases";
import { useAutoRefresh } from "@/shared/lib";
import { AutoRefreshToggle } from "@/shared/ui";

const route = useRoute();
const vendor = computed(() => String(route.params.vendor));
const app = computed(() => String(route.params.app));
const auto = useAutoRefresh();
const record = useApp(vendor, app);
</script>

<template>
  <div class="flex flex-col gap-6">
    <div class="flex justify-end">
      <AutoRefreshToggle v-model="auto.enabled.value" />
    </div>
    <ReleaseLookupForm
      v-if="record.data.value?.provider === 'json-release'"
      :vendor="vendor"
      :app="app"
    />
    <VersionList :vendor="vendor" :app="app" :auto-refresh="auto.enabled.value" />
  </div>
</template>
