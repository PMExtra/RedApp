<script setup lang="ts">
import AppShell from "../components/AppShell.vue";
import Icon from "../components/Icon.vue";
import { t } from "../i18n";
</script>
<template>
  <AppShell
    ><template #actions
      ><RouterLink to="/admin/overview" class="header-link admin-link"
        ><Icon name="user" /><span>{{ t("Administrator") }}</span></RouterLink
      ></template
    >
    <main id="main-content" class="public-main" tabindex="-1">
      <RouterView /></main
  ></AppShell>
</template>
