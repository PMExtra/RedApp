<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { HomepagePinsForm, PublicUrlForm, SiteTextsForm } from "@/features/settings";
import { PageHeader } from "@/shared/ui";

const { t } = useI18n();
</script>

<template>
  <div class="flex flex-col gap-6">
    <PageHeader
      :title="t('adminShell.titles.site')"
      :description="t('siteSettingsPage.description')"
    />
    <SiteTextsForm />
    <PublicUrlForm />
    <HomepagePinsForm />
  </div>
</template>
