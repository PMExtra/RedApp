<script setup lang="ts">
import { computed } from "vue";
import { ArrowRight } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import { AppGrid, useHome } from "@/features/catalog";
import { AsyncState, Button, PageHeader } from "@/shared/ui";

const { t } = useI18n();
const home = useHome();
// The ranking only orders the apps; download counts are not shown publicly.
const popular = computed(() => home.data.value?.ranking.map((entry) => entry.app) ?? []);
// Featured apps are curated by administrators; an empty list is simply not shown.
const sections = computed(() => [
  ...(home.data.value?.pinned.length
    ? [{ id: "pinned", title: t("catalog.home.pinned"), apps: home.data.value.pinned }]
    : []),
  { id: "popular", title: t("catalog.home.popular"), apps: popular.value },
]);
</script>

<template>
  <div class="flex flex-col gap-8">
    <PageHeader :title="t('catalog.home.title')">
      <template #actions>
        <Button as-child variant="secondary">
          <RouterLink to="/all">
            {{ t("catalog.home.browseAll") }}
            <ArrowRight aria-hidden="true" />
          </RouterLink>
        </Button>
      </template>
    </PageHeader>
    <AsyncState v-if="!home.data.value" :loading="home.isPending.value" :error="home.error.value" @retry="home.refetch()">
      <template #loading>
        <AppGrid loading :placeholders="3" />
      </template>
    </AsyncState>
    <template v-else>
      <section
        v-for="section in sections"
        :key="section.id"
        :aria-labelledby="`home-${section.id}`"
        class="flex flex-col gap-4"
      >
        <h2 :id="`home-${section.id}`" class="text-lg font-semibold">{{ section.title }}</h2>
        <AppGrid v-if="section.apps.length" :apps="section.apps" />
        <p v-else class="rounded-xl border border-dashed border-border px-4 py-8 text-center text-sm text-muted">
          {{ t("catalog.home.popularEmpty") }}
        </p>
      </section>
    </template>
  </div>
</template>
