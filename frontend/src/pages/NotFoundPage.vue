<script setup lang="ts">
import AppShell from "../components/AppShell.vue";
import { t } from "../i18n";
</script>
<template>
  <AppShell
    ><main id="main-content" class="public-main" tabindex="-1">
      <h1>{{ t("Page not found") }}</h1>
      <RouterLink to="/">{{ t("All applications") }}</RouterLink>
    </main></AppShell
  >
</template>
