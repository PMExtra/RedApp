<script setup lang="ts">
import { LayoutGrid, Shield } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { RouterLink, RouterView, useRoute } from "vue-router";
import { SiteFooter, useSiteTexts } from "@/features/bootstrap";
import { useAdminLink } from "@/features/catalog";
import { SearchBox } from "@/features/search";
import { useSiteTitle } from "@/shared/lib";
import {
  Alert,
  Button,
  LanguageSwitcher,
  Skeleton,
  SkipLink,
  ThemeToggle,
  Tooltip,
  TopNav,
} from "@/shared/ui";

const { t, locale } = useI18n();
const { bootstrap, title, subtitle } = useSiteTexts();
useSiteTitle(title);
const route = useRoute();
// On vendor and application pages the admin link opens their admin page.
const adminHref = useAdminLink(() => {
  const { vendor, app } = route.params;
  if (typeof vendor !== "string") return null;
  if (route.name === "public-vendor") return { vendor };
  return route.name === "public-app" && typeof app === "string" ? { vendor, app } : null;
});
</script>

<template>
  <div class="flex min-h-dvh flex-col">
    <SkipLink />
    <TopNav>
      <template #brand>
        <RouterLink to="/" class="flex flex-col rounded-md leading-tight focus-ring">
          <Skeleton v-if="bootstrap.isPending.value" class="h-5 w-28" />
          <template v-else>
            <span class="text-base font-semibold">{{ title }}</span>
            <span v-if="subtitle" class="hidden text-xs text-muted md:block">{{ subtitle }}</span>
          </template>
        </RouterLink>
      </template>
      <SearchBox />
      <template #actions>
        <span class="hidden sm:contents">
          <Button as-child variant="ghost" size="sm">
            <RouterLink to="/all">
              <LayoutGrid aria-hidden="true" />
              {{ t("publicShell.nav.all") }}
            </RouterLink>
          </Button>
        </span>
        <LanguageSwitcher />
        <ThemeToggle />
        <!-- A full page load: the admin app is a separate entry (admin.html). -->
        <Tooltip :content="t('publicShell.nav.admin')">
          <Button as-child variant="ghost" size="sm" icon>
            <a :href="adminHref" :aria-label="t('publicShell.nav.admin')">
              <Shield aria-hidden="true" />
            </a>
          </Button>
        </Tooltip>
      </template>
    </TopNav>
    <nav
      v-if="bootstrap.data.value?.site.navigation?.length"
      :aria-label="t('publicShell.nav.custom')"
      class="border-b border-border bg-surface"
    >
      <div class="mx-auto flex max-w-7xl flex-wrap gap-x-5 gap-y-2 px-4 py-3 text-sm sm:px-6">
        <a
          v-for="(item, index) in bootstrap.data.value.site.navigation"
          :key="index"
          :href="item.url"
          :target="item.new_tab ? '_blank' : undefined"
          :rel="item.new_tab ? 'noopener noreferrer' : undefined"
          class="max-w-full break-words rounded-sm text-primary hover:underline focus-ring"
        >
          {{
            item.localized_title?.[locale === "zh-CN" ? "zh-CN" : "en"] ||
            item.localized_title?.[locale === "zh-CN" ? "en" : "zh-CN"] ||
            item.title
          }}<span v-if="item.new_tab" class="sr-only"> {{ t("publicShell.nav.newTab") }}</span>
        </a>
      </div>
    </nav>
    <div v-if="bootstrap.isError.value" class="mx-auto w-full max-w-7xl px-4 pt-4 sm:px-6">
      <Alert tone="warning" :title="t('publicShell.siteUnavailable')">
        <template #actions>
          <Button size="sm" @click="bootstrap.refetch()">{{ t("common.actions.retry") }}</Button>
        </template>
      </Alert>
    </div>
    <main
      id="main-content"
      tabindex="-1"
      class="mx-auto w-full max-w-7xl flex-1 px-4 py-8 outline-none sm:px-6"
    >
      <RouterView />
    </main>
    <SiteFooter />
  </div>
</template>
