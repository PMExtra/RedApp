<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRoute } from "vue-router";
import { AppVersion, DownloadPrefix, HostedFiles, usePublicApp } from "@/features/catalog";
import { InstructionsDocument } from "@/features/instructions";
import { isApiError } from "@/shared/api";
import { useLocalized, type Locale } from "@/shared/i18n";
import { useDocumentTitle } from "@/shared/lib";
import { AsyncState, Card, EntityIcon, PageHeader, Skeleton } from "@/shared/ui";
import NotFoundPage from "./NotFoundPage.vue";

/** `/{vendor}/{app}`: identity, version, usage instructions and downloads. */
const { t, locale } = useI18n();
const localized = useLocalized();
const route = useRoute();
const vendor = computed(() => (typeof route.params.vendor === "string" ? route.params.vendor : ""));
const appId = computed(() => (typeof route.params.app === "string" ? route.params.app : ""));
const query = usePublicApp(vendor, appId);
const app = computed(() => query.data.value);

const notFound = computed(
  () => isApiError(query.error.value) && query.error.value.code === "APPLICATION_NOT_FOUND",
);
const name = computed(() => (app.value ? localized(app.value.name) || app.value.id : ""));
const vendorName = computed(() =>
  app.value ? localized(app.value.vendor.name) || app.value.vendor.id : vendor.value,
);
useDocumentTitle(() => (notFound.value ? t("publicShell.titles.notFound") : name.value));

const capabilities = computed(() => app.value?.capabilities);
// The instructions follow the UI language; a language without text shows none.
const instructions = computed(
  () =>
    !!capabilities.value?.instructions &&
    !!app.value?.instructions_available[locale.value as Locale],
);
const hosted = computed(() => !!capabilities.value?.hosted_files);
const prefix = computed(
  () => !!capabilities.value?.files && !capabilities.value.installers && !hosted.value,
);
</script>

<template>
  <NotFoundPage v-if="notFound" />
  <div v-else class="flex flex-col gap-6">
    <AsyncState
      v-if="!app"
      :loading="query.isPending.value"
      :error="query.error.value"
      @retry="query.refetch()"
    >
      <template #loading>
        <div class="flex flex-col gap-4">
          <Skeleton class="h-4 w-56" />
          <div class="flex items-center gap-4">
            <Skeleton class="size-16 rounded-lg" />
            <div class="flex flex-1 flex-col gap-2">
              <Skeleton class="h-7 w-48" />
              <Skeleton class="h-4 w-80 max-w-full" />
            </div>
          </div>
          <Skeleton class="h-40 w-full rounded-xl" />
        </div>
      </template>
    </AsyncState>
    <template v-else>
      <PageHeader
        :title="name"
        :description="localized(app.description)"
        :breadcrumbs="[
          { label: t('catalog.list.title'), to: '/all' },
          { label: vendorName, to: `/${app.vendor.id}` },
          { label: name },
        ]"
      >
        <template #media>
          <EntityIcon :src="app.icon" size="xl" />
        </template>
        <template v-if="capabilities?.versions || app.categories.length" #meta>
          <AppVersion :app="app" />
          <ul v-if="app.categories.length" class="flex flex-wrap gap-1.5">
            <li v-for="item in app.categories" :key="item.id">
              <RouterLink
                :to="{ path: '/all', query: { category: item.id } }"
                class="inline-flex rounded-full border border-border px-2 py-0.5 text-xs text-muted hover:bg-surface-hover hover:text-fg focus-ring"
              >
                {{ localized(item.name) || item.id }}
              </RouterLink>
            </li>
          </ul>
        </template>
      </PageHeader>

      <Card v-if="instructions" :title="t('catalog.app.instructions')">
        <InstructionsDocument :vendor="app.vendor.id" :app="app.id" :revision="app.revision" />
      </Card>
      <HostedFiles v-if="hosted" :app="app" />
      <DownloadPrefix v-else-if="prefix" :app-key="app.key" />
    </template>
  </div>
</template>
