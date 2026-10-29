<script setup lang="ts">
// Stored files of one version, below the versions tab (AppLayout keeps that
// tab selected and adds the version to the breadcrumbs). A JSON release's
// version can be re-fetched from here.
import { computed } from "vue";
import { ArrowLeft } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRoute } from "vue-router";
import { useApp } from "@/features/directory";
import { ReleaseRefetchButton } from "@/features/json-release";
import { VersionFiles } from "@/features/releases";
import { useAutoRefresh } from "@/shared/lib";
import { AutoRefreshToggle, Button } from "@/shared/ui";

const { t } = useI18n();
const route = useRoute();
const vendor = computed(() => String(route.params.vendor));
const app = computed(() => String(route.params.app));
const version = computed(() => String(route.params.version));
const auto = useAutoRefresh();
const record = useApp(vendor, app);
</script>

<template>
  <div class="flex flex-col gap-6">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <Button size="sm" variant="ghost" as-child>
        <RouterLink :to="{ name: 'admin-app-versions', params: { vendor, app } }">
          <ArrowLeft aria-hidden="true" /> {{ t("releases.resources.back") }}
        </RouterLink>
      </Button>
      <div class="flex flex-wrap items-center gap-3">
        <ReleaseRefetchButton
          v-if="record.data.value?.provider === 'json-release' && !record.data.value.deleted_at"
          :vendor="vendor"
          :app="app"
          :version="version"
        />
        <AutoRefreshToggle v-model="auto.enabled.value" />
      </div>
    </div>
    <VersionFiles
      :vendor="vendor"
      :app="app"
      :version="version"
      :auto-refresh="auto.enabled.value"
    />
  </div>
</template>
