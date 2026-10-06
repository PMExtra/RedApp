<script setup lang="ts">
import ApplicationVersion from "./components/ApplicationVersion.vue";
import { applicationVendorName } from "./vendorName";
import PublicCards from "./components/PublicCards.vue";
import IconButton from "./components/IconButton.vue";
import { computed, watchEffect } from "vue";
import { useRoute } from "vue-router";
import {
  bootstrap,
  bootstrapError,
  bootstrapLoading,
  loadBootstrap,
  applicationCapabilities,
} from "./bootstrap";
import { language, t } from "./i18n";
import { siteTitle } from "./site";
import HostedDownloads from "./components/HostedDownloads.vue";
import InstructionsDocument from "./components/InstructionsDocument.vue";
import Icon from "./components/Icon.vue";
const route = useRoute();
const selectedID = computed(
  () => `${route.params.vendor}/${route.params.app}`,
);
const detail = computed(() => !!route.params.vendor);
const selected = computed(() =>
  bootstrap.value?.apps.find((app) => app.id === selectedID.value),
);
watchEffect(() => {
  document.title = `${detail.value ? selected.value?.name[language.value] || t("Applications") : t("Applications")} · ${siteTitle.value}`;
});
</script>
<template>
  <div v-if="!bootstrap && bootstrapLoading" class="empty panel" role="status">
    {{ t("Loading applications…") }}
  </div>
  <section
    v-else-if="!bootstrap && bootstrapError"
    class="panel empty-state"
    role="alert"
  >
    <Icon name="alert" :size="28" />
    <h1>{{ t("Unable to load applications") }}</h1>
    <p class="muted">
      {{ t("Connection failed. Check your connection and retry.") }}
    </p>
    <IconButton @click="loadBootstrap" icon="refresh" :label="t('Retry')" />
  </section>
  <template v-else-if="detail && selected && bootstrap"
    ><nav class="breadcrumbs" :aria-label="t('Breadcrumb')">
      <RouterLink to="/all">{{ t("All applications") }}</RouterLink
      ><span>›</span
      ><RouterLink :to="`/${route.params.vendor}`">{{
        applicationVendorName(selected)
      }}</RouterLink
      ><span>›</span><span>{{ selected.name[language] }}</span>
    </nav>
    <div class="application-identity">
      <div class="application-logo">
        <img
          v-if="selected.icon"
          :src="selected.icon"
          :alt="applicationVendorName(selected)"
          width="48"
          height="48"
        /><Icon v-else name="box" :size="48" />
      </div>
      <div class="public-heading">
        <span
          v-if="!applicationCapabilities(selected).installers"
          class="eyebrow"
        >{{
          selected.provider === "info"
            ? t("Usage instructions")
            : t("Download files")
        }}</span>
        <h1>{{ selected.name[language] }}</h1>
        <p class="public-lead">{{ selected.summary[language] }}</p>
        <ApplicationVersion :app="selected" />
      </div>
    </div>
    <section
      v-if="selected.instructions?.[language]"
      class="panel usage-instructions"
    >
      <h2>{{ t("Usage instructions") }}</h2>
      <InstructionsDocument
        :application="selected.id"
        :revision="bootstrap.revision"
      />
    </section>
    <HostedDownloads
      v-if="selected.provider === 'hosted'"
      :application="selected.id"
    />
    <section
      v-else-if="
        applicationCapabilities(selected).files &&
        !applicationCapabilities(selected).installers
      "
      class="panel download-prefix"
    >
      <h2>{{ t("Download URL prefix") }}</h2>
      <pre tabindex="0">{{ bootstrap.public_origin }}/{{ selected.id }}/</pre>
      <p class="muted">
        {{
          t(
            "Append the relative file path to this address. Files are fetched and cached when requested.",
          )
        }}
      </p>
    </section></template
  >
  <section v-else-if="detail" class="panel empty-state">
    <h1>{{ t("Page not found") }}</h1>
    <RouterLink to="/">{{ t("All applications") }}</RouterLink>
  </section>
  <template v-else
    ><div class="public-heading">
      <h1>{{ t("Applications") }}</h1>
    </div>
    <PublicCards :apps="bootstrap?.apps || []" show-action />
    <p v-if="bootstrap && !bootstrap.apps.length" class="empty">
      {{ t("No applications are available.") }}
    </p></template
  >
</template>
