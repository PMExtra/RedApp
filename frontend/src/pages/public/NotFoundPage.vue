<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import { Button, EmptyState } from "@/shared/ui";

const { t } = useI18n();
</script>

<template>
  <div class="flex flex-col items-center">
    <h1 class="sr-only">{{ t("publicShell.notFound.title") }}</h1>
    <EmptyState :title="t('publicShell.notFound.title')" :description="t('publicShell.notFound.description')">
      <template #action>
        <Button as-child variant="primary">
          <RouterLink to="/">{{ t("publicShell.notFound.home") }}</RouterLink>
        </Button>
      </template>
    </EmptyState>
  </div>
</template>
