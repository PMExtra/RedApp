<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { GlobalProxyForm } from "@/features/settings";
import { PageHeader } from "@/shared/ui";

const { t } = useI18n();
</script>

<template>
  <div class="flex flex-col gap-6">
    <PageHeader
      :title="t('adminShell.titles.proxy')"
      :description="t('proxySettingsPage.description')"
    />
    <GlobalProxyForm />
  </div>
</template>
