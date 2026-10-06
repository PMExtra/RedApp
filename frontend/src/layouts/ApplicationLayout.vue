<script setup lang="ts">
import EntityIcon from "../components/EntityIcon.vue";
import IconButton from "../components/IconButton.vue";
import { computed, onUnmounted, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { validApplicationID, bootstrap } from "../bootstrap";
import {
  directoryIcon,
  applicationRecord as application,
  vendorRecord as vendor,
  directoryLoading,
  directoryError,
  loadApplication,
  resetDirectory,
  applicationPath,
  applicationEnabled,
  providerHasVersions,
} from "../directory";
import { errorText, language, t } from "../i18n";
import Icon from "../components/Icon.vue";
const route = useRoute(),
  copied = ref(false),
  copyError = ref<unknown>();
const key = computed(() => `${route.params.vendor}/${route.params.app}`);
watch(
  key,
  (value) => {
    copied.value = false;
    copyError.value = undefined;
    if (validApplicationID(value)) void loadApplication(value);
    else resetDirectory();
  },
  { immediate: true, flush: "sync" },
);
onUnmounted(resetDirectory);
const available = computed(() => {
  const app = application.value;
  if (!app) return false;
  if (route.path.endsWith("/settings") || route.path.endsWith("/admin-notes")) return true;
  if (route.path.endsWith("/files")) return app.provider === "hosted";
  if (route.path.endsWith("/cache"))
    return !["info", "hosted"].includes(app.provider);
  return (
    route.path.endsWith("/versions") &&
    !app.deleted_at &&
    providerHasVersions(app.provider)
  );
});
async function copyURL() {
  try {
    await navigator.clipboard.writeText(
      `${bootstrap.value?.public_origin || window.location.origin}/${key.value}/`,
    );
    copied.value = true;
  } catch (reason) {
    copyError.value = reason;
  }
}
</script>
<template>
  <p v-if="directoryLoading && !application" role="status">
    {{ t("Loading applications…") }}
  </p>
  <div v-else-if="directoryError && !application" class="error" role="alert">
    {{ errorText(directoryError)
    }}<IconButton
      class="secondary"
      @click="loadApplication(key)"
      icon="refresh"
      :label="t('Retry')"
    />
  </div>
  <template v-else-if="application && vendor">
    <div v-if="directoryError" class="error" role="alert">
      {{ errorText(directoryError)
      }}<IconButton
        class="secondary"
        @click="loadApplication(key, true)"
        icon="refresh"
        :label="t('Retry')"
      />
    </div>
    <nav class="breadcrumbs" :aria-label="t('Application location')">
      <RouterLink to="/admin/vendors">{{
        t("Vendors and applications")
      }}</RouterLink
      ><span aria-hidden="true">/</span
      ><RouterLink :to="`/admin/vendors/${vendor.id}/apps`">{{
        vendor.name[language]
      }}</RouterLink
      ><span aria-hidden="true">/</span
      ><span>{{ application.name[language] }}</span>
    </nav>
    <header class="application-header">
      <div class="application-header-identity">
        <EntityIcon :src="directoryIcon(application.icon)" size="detail" />
        <div>
          <h1>{{ application.name[language] }}</h1>

        </div>
      </div>
      <div v-if="applicationEnabled(application)" class="form-actions">
        <RouterLink
          :to="`/${application.key}`"
          class="button-link secondary"
          >{{
            providerHasVersions(application.provider)
              ? t("Installation instructions")
              : t("Public application page")
          }}</RouterLink
        ><IconButton
          v-if="application.provider !== 'info'"
          class="secondary"
          @click="copyURL"
          icon="copy"
          :label="copied ? t('Copied') : t('Copy download URL')"
        />
      </div>
    </header>
    <p v-if="copyError" class="error" role="alert">
      {{ errorText(copyError) }}
    </p>
    <nav class="application-tabs" :aria-label="t('Application sections')">
      <RouterLink
        v-if="application.provider === 'hosted'"
        :to="applicationPath(application, 'files')"
        >{{ t("Hosted files") }}</RouterLink
      >
      <RouterLink
        v-if="
          !application.deleted_at && providerHasVersions(application.provider)
        "
        :to="applicationPath(application, 'versions')"
        >{{ t("Versions and resources") }}</RouterLink
      >
      <RouterLink
        v-if="!['info', 'hosted'].includes(application.provider)"
        :to="applicationPath(application, 'cache')"
        >{{ t("Cache management") }}</RouterLink
      >
      <RouterLink :to="applicationPath(application, 'settings')">{{
        t("Application settings")
      }}</RouterLink>
      <RouterLink :to="applicationPath(application, 'admin-notes')">{{ t('Admin Notes') }}</RouterLink>
    </nav>
    <RouterView v-if="available" :key="application.uid" />
    <p v-else>{{ t("Page not found") }}</p>
  </template>
  <p v-else>{{ t("Page not found") }}</p>
</template>
