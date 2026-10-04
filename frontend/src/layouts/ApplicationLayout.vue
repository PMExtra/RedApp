<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";
import { validApplicationID } from "../bootstrap";
import { managedApps, directoryLoading, directoryError, loadDirectory, providerHasVersions, providerHasTimeCleanup } from "../directory";
import { errorText, language, t } from "../i18n";
const route = useRoute();
const application = computed(() => {
  const id = `${route.params.vendor}/${route.params.app}`;
  return validApplicationID(id)
    ? managedApps.value.find((app) => app.key === id)
    : undefined;
});
const pageAvailable = computed(() => !!application.value && (
  route.path.endsWith('/settings') || (
    route.path.endsWith('/versions') && !application.value.deleted_at && providerHasVersions(application.value.provider) ||
    route.path.endsWith('/cache') && providerHasTimeCleanup(application.value.provider)
  )
));
</script>
<template>
  <template v-if="application"
    ><div class="page-heading">
      <h1>{{ application.name[language] }}</h1>
    </div>
    <RouterView v-if="pageAvailable"
  /><p v-else>{{ t('Page not found') }} <RouterLink :to="`/admin/apps/${application.key}/settings`">{{ t('Settings') }}</RouterLink></p></template>
  <div v-else-if="directoryError" class="error" role="alert">{{ errorText(directoryError) }}<button class="secondary" :disabled="directoryLoading" @click="loadDirectory">{{ t('Retry') }}</button></div>
  <p v-else role="status">
    {{ directoryLoading ? t("Loading applications…") : t("Page not found") }}
  </p>
</template>
