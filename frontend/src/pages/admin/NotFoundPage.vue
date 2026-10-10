<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import { Button, EmptyState } from "@/shared/ui";

const { t } = useI18n();
</script>

<template>
  <div>
    <h1 class="sr-only">{{ t("adminShell.titles.notFound") }}</h1>
    <EmptyState
      :title="t('adminShell.titles.notFound')"
      :description="t('adminShell.notFound.description')"
    >
      <template #action>
        <Button as-child variant="primary">
          <RouterLink :to="{ name: 'admin-overview' }">{{
            t("adminShell.notFound.back")
          }}</RouterLink>
        </Button>
      </template>
    </EmptyState>
  </div>
</template>
