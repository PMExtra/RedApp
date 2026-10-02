<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";
import { bootstrap, bootstrapLoading, validApplicationID } from "../bootstrap";
import { language, t } from "../i18n";
const route = useRoute();
const application = computed(() => {
  const id = `${route.params.vendor}/${route.params.app}`;
  return validApplicationID(id)
    ? bootstrap.value?.apps.find((app) => app.id === id)
    : undefined;
});
</script>
<template>
  <template v-if="application"
    ><div class="page-heading">
      <h1>{{ application.name[language] }}</h1>
    </div>
    <RouterView
  /></template>
  <p v-else role="status">
    {{ bootstrapLoading ? t("Loading applications…") : t("Page not found") }}
  </p>
</template>
