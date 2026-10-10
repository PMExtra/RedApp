<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { Menu } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { RouterLink, RouterView, useRoute, useRouter } from "vue-router";
import { SiteFooter, useSiteTexts } from "@/features/bootstrap";
import { AccountMenu, SessionExpiredDialog, useSessionStore } from "@/features/session";
import { leaveDiscardingDrafts } from "@/shared/forms";
import { useSiteTitle } from "@/shared/lib";
import {
  Badge,
  Dialog,
  IconButton,
  LanguageSwitcher,
  SideNav,
  SkipLink,
  ThemeToggle,
  TopNav,
} from "@/shared/ui";
import { adminNavigation } from "./navigation";

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const session = useSessionStore();
const { title } = useSiteTexts();
useSiteTitle(computed(() => `${t("adminShell.title")} · ${title.value}`));

const navigation = computed(() => adminNavigation(t));
const menuOpen = ref(false);
watch(
  () => route.fullPath,
  () => {
    menuOpen.value = false;
  },
);

// Signed out (sign-out or password change): go to sign-in. The account menu
// confirmed the drafts before the action, and without a session they could
// not be saved anyway, so the leave guards must not hold the user here.
watch(
  () => session.status,
  async (status) => {
    if (status === "signedOut" && !route.meta.public) {
      const returnTo = route.fullPath;
      await leaveDiscardingDrafts(() =>
        router.replace({ name: "admin-login", query: { returnTo } }),
      );
    }
  },
);

// The spec: re-read the session whenever the tab becomes visible.
function onVisibility() {
  if (document.visibilityState === "visible" && session.status === "signedIn") {
    void session.check();
  }
}
onMounted(() => {
  document.addEventListener("visibilitychange", onVisibility);
});
onBeforeUnmount(() => {
  document.removeEventListener("visibilitychange", onVisibility);
});
</script>

<template>
  <div class="flex min-h-dvh flex-col">
    <SkipLink />
    <TopNav>
      <template #brand>
        <span v-if="!route.meta.public" class="md:hidden">
          <IconButton :label="t('adminShell.openMenu')" size="sm" @click="menuOpen = true">
            <Menu aria-hidden="true" />
          </IconButton>
        </span>
        <RouterLink
          :to="{ name: 'admin-overview' }"
          class="flex items-center gap-2 rounded-md font-semibold focus-ring"
        >
          {{ title }}
          <Badge tone="primary">{{ t("adminShell.badge") }}</Badge>
        </RouterLink>
      </template>
      <template #actions>
        <LanguageSwitcher />
        <ThemeToggle />
        <AccountMenu v-if="session.signedIn" />
      </template>
    </TopNav>
    <div class="mx-auto flex w-full max-w-7xl flex-1 gap-8 px-4 sm:px-6">
      <aside v-if="!route.meta.public" class="hidden w-56 shrink-0 py-8 md:block">
        <SideNav :sections="navigation" :label="t('ui.nav.main')" class="sticky top-22" />
      </aside>
      <main id="main-content" tabindex="-1" class="min-w-0 flex-1 py-8 outline-none">
        <RouterView />
      </main>
    </div>
    <SiteFooter show-platform />
    <Dialog v-model:open="menuOpen" :title="t('ui.nav.main')" size="sm">
      <SideNav :sections="navigation" :label="t('ui.nav.main')" />
    </Dialog>
    <SessionExpiredDialog />
  </div>
</template>
