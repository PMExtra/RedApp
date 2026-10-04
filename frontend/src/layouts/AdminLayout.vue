<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch, watchEffect } from "vue";
import { useRoute, useRouter } from "vue-router";
import AppShell from "../components/AppShell.vue";
import AccountMenu from "../components/AccountMenu.vue";
import PasswordDialog from "../components/PasswordDialog.vue";
import Icon from "../components/Icon.vue";
import { managedApps, loadDirectory, resetDirectory, providerHasVersions, providerHasTimeCleanup } from "../directory";
import {
  checkSession,
  cancelSessionCheck,
  expireSession,
  logoutSession,
  sessionBusy,
  sessionChecked,
  sessionError,
  sessionNotice,
  signedIn,
} from "../session";
import { errorText, language, t } from "../i18n";
import { siteTitle } from "../site";
import { confirmDirtyDrafts } from "../composables/useDirtyDraft";
const router = useRouter(),
  route = useRoute();
const passwordOpen = ref(false),
  busy = ref(false),
  actionError = ref<unknown>();
function redirect() {
  if (sessionChecked.value && !signedIn.value && route.path !== "/admin/login")
    void router.replace("/admin/login");
  if (signedIn.value && route.path === "/admin/login")
    void router.replace("/admin/overview");
}
watch([signedIn, sessionChecked, () => route.path], () => {
  if (!signedIn.value) passwordOpen.value = false;
  redirect();
});
watch(signedIn, (active) => {
  if (active) void loadDirectory();
  else resetDirectory();
}, { immediate: true });
watchEffect(() => {
  document.title = `${t("Administration")} · ${siteTitle.value}`;
});
async function logout() {
  if (busy.value || !confirmDirtyDrafts()) return;
  busy.value = true;
  actionError.value = undefined;
  try {
    await logoutSession();
  } catch (error) {
    actionError.value = error;
  } finally {
    busy.value = false;
  }
}
const visible = () => {
  if (document.visibilityState !== "hidden") void checkSession();
};
onMounted(() => {
  void checkSession();
  document.addEventListener("visibilitychange", visible);
});
onUnmounted(() => {
  cancelSessionCheck();
  document.removeEventListener("visibilitychange", visible);
  passwordOpen.value = false;
  resetDirectory();
});
</script>
<template>
  <AppShell admin
    ><template #actions
      ><AccountMenu
        v-if="signedIn"
        :busy="busy"
        @password="passwordOpen = true"
        @logout="logout"
      /><RouterLink v-else to="/" class="header-link">{{
        t("Applications")
      }}</RouterLink></template
    >
    <div :class="['admin-layout', { 'is-signed-out': !signedIn }]">
      <aside v-if="signedIn" class="sidebar">
        <span class="eyebrow">{{ t("Administration") }}</span>
        <nav :aria-label="t('Administration')">
          <RouterLink to="/admin/overview"
            ><Icon name="grid" />{{ t("Overview") }}</RouterLink
          >
          <RouterLink to="/admin/events"
            ><Icon name="activity" />{{ t("Events") }}</RouterLink
          >
          <RouterLink to="/admin/settings/site"
            ><Icon name="settings" />{{ t("Site appearance") }}</RouterLink
          >
          <RouterLink to="/admin/settings/proxy"
            ><Icon name="settings" />{{ t("Upstream proxy") }}</RouterLink
          >
          <RouterLink to="/admin/vendors"><Icon name="box" />{{ t("Vendors and applications") }}</RouterLink>
          <template v-for="app in managedApps.filter((item) => !item.deleted_at)" :key="app.uid"
            ><span class="sidebar-app-name">{{ app.name[language] }}</span
            ><RouterLink v-if="providerHasVersions(app.provider)" :to="`/admin/apps/${app.key}/versions`"
              ><Icon name="box" />{{ t("Versions") }}</RouterLink
            ><RouterLink v-if="providerHasTimeCleanup(app.provider)" :to="`/admin/apps/${app.key}/cache`"
              ><Icon name="box" />{{ t("Cache") }}</RouterLink
            ><RouterLink :to="`/admin/apps/${app.key}/settings`"
              ><Icon name="settings" />{{ t("Settings") }}</RouterLink
            ></template
          >
        </nav>
        <RouterLink to="/" class="sidebar-public"
          ><Icon name="arrow" :size="16" />{{
            t("Public installation page")
          }}</RouterLink
        >
      </aside>
      <main
        id="main-content"
        :class="['admin-main', { 'auth-main': !signedIn }]"
        tabindex="-1"
      >
        <p v-if="sessionNotice" class="notice" role="status">
          {{ t(sessionNotice) }}
        </p>
        <div v-if="sessionError || actionError" class="error" role="alert">
          {{ errorText(sessionError || actionError)
          }}<button
            v-if="sessionError"
            class="secondary"
            :disabled="sessionBusy"
            @click="checkSession"
          >
            {{ t("Retry") }}
          </button>
        </div>
        <p v-if="!sessionChecked" role="status">{{ t("Loading…") }}</p>
        <RouterView v-else-if="signedIn || route.path === '/admin/login'" />
      </main></div
  ></AppShell>
  <PasswordDialog
    v-if="passwordOpen && signedIn"
    @close="passwordOpen = false"
    @changed="expireSession('Password changed. Sign in again.')"
    @error="actionError = $event"
  />
</template>
