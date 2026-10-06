<script setup lang="ts">
import AppShell from "../components/AppShell.vue";
import Icon from "../components/Icon.vue";
import { computed } from "vue";
import { useRoute } from "vue-router";
import { bootstrap, validApplicationID } from "../bootstrap";
import { applicationPath } from "../directory";
import { t } from "../i18n";
const route = useRoute();
const adminTarget = computed(() => {
  const key = `${route.params.vendor}/${route.params.app}`;
  if (route.name !== "public-application" || !validApplicationID(key)) return "/admin/overview";
  const app = bootstrap.value?.apps.find(app => app.id === key);
  if (!app) return "/admin/overview";
  return applicationPath({ vendor_id: String(route.params.vendor), id: String(route.params.app), provider: app.provider || "info" });
});
</script>
<template>
  <AppShell
    ><template #actions
      ><RouterLink :to="adminTarget" class="header-link admin-link"
        ><Icon name="user" /><span>{{ t("Administrator") }}</span></RouterLink
      ></template
    >
    <main id="main-content" class="public-main" tabindex="-1">
      <RouterView /></main
  ></AppShell>
</template>
