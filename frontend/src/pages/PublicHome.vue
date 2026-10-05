<script setup lang="ts">
import IconButton from "../components/IconButton.vue";
import { ref } from "vue";
import type { Application } from "../bootstrap";
import { usePublicResource } from "../public";
import { errorText, t } from "../i18n";
import PublicCards from "../components/PublicCards.vue";
const { data, error, loading, refresh } = usePublicResource<{
  pinned: Application[];
  ranking: (Application & { download_clients: number })[];
}>(
  ref("/api/home"),
  (value) => Array.isArray(value.pinned) && Array.isArray(value.ranking),
);
</script>
<template>
  <div class="page-heading">
    <h1>{{ t("Applications") }}</h1>
    <RouterLink to="/all" class="button-link">{{
      t("All applications")
    }}</RouterLink>
  </div>
  <p v-if="error" role="alert" class="error">
    {{ errorText(error) }}
    <IconButton @click="refresh" icon="refresh" :label="t('Retry')" />
  </p>
  <p v-if="loading" role="status">{{ t("Loading…") }}</p>
  <template v-if="data"
    ><section class="public-section">
      <h2>{{ t("Pinned applications") }}</h2>
      <PublicCards :apps="data.pinned" />
      <p v-if="!data.pinned.length" class="muted">
        {{ t("No pinned applications.") }}
      </p>
    </section>
    <section class="public-section">
      <h2>{{ t("Popular downloads") }}</h2>
      <p class="muted">
        {{
          t(
            "Approximate unique download clients over the last seven days, using hourly summaries.",
          )
        }}
      </p>
      <PublicCards :apps="data.ranking" />
      <p v-if="!data.ranking.length" class="muted">
        {{ t("No downloads yet.") }}
      </p>
    </section></template
  >
</template>
