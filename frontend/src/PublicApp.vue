<script setup lang="ts">
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
import InstallCommands from "./components/InstallCommands.vue";
import Icon from "./components/Icon.vue";
const route = useRoute();
const selectedID = computed(() => `${route.params.vendor}/${route.params.app}`);
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
    <button @click="loadBootstrap">{{ t("Retry") }}</button>
  </section>
  <template v-else-if="detail && selected && bootstrap"
    ><RouterLink to="/" class="back-link"
      ><Icon name="back" :size="16" />{{ t("All applications") }}</RouterLink
    >
    <div class="application-identity">
      <div class="application-logo">
        <img
          v-if="selected.icon"
          :src="selected.icon"
          :alt="selected.publisher"
          width="48"
          height="48"
        /><Icon v-else name="box" :size="48" />
      </div>
      <div class="public-heading">
        <span class="eyebrow">{{ applicationCapabilities(selected).installers ? t("Installation instructions") : t("Download files") }}</span>
        <h1>{{ selected.name[language] }}</h1>
        <p class="public-lead">{{ selected.summary[language] }}</p>
      </div>
    </div>
    <div v-if="applicationCapabilities(selected).installers" class="installation-layout">
      <InstallCommands
        :origin="bootstrap.public_origin"
        :application="selected"
      />
      <aside class="install-guide">
        <section class="panel">
          <h2>{{ t("Getting started") }}</h2>
          <ol class="steps">
            <li>
              {{
                t(
                  "Open a terminal on Linux or macOS, or PowerShell on Windows.",
                )
              }}
            </li>
            <li>
              {{
                t(
                  "Copy and run the matching command. Review the prompts before confirming installation.",
                )
              }}
            </li>
          </ol>
        </section>
      </aside>
    </div>
    <section v-else class="panel download-prefix">
      <h2>{{ t("Download URL prefix") }}</h2>
      <pre tabindex="0">{{ bootstrap.public_origin }}/{{ selected.id }}/</pre>
      <p class="muted">{{ t("Append the relative file path to this address. Files are fetched and cached when requested.") }}</p>
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
    <div class="application-list">
      <RouterLink
        v-for="app in bootstrap?.apps || []"
        :key="app.id"
        :to="`/${app.id}`"
        class="application-card"
        ><div class="application-card-brand">
          <div class="application-logo">
            <img
              v-if="app.icon"
              :src="app.icon"
              :alt="app.publisher"
              width="40"
              height="40"
            /><Icon v-else name="box" :size="40" />
          </div>
          <span class="app-publisher">{{ app.publisher }}</span>
        </div>
        <h2>{{ app.name[language] }}</h2>
        <p>{{ app.summary[language] }}</p>
        <span class="card-action"
          >{{ applicationCapabilities(app).installers ? t("Installation instructions") : t("Download files") }}<Icon name="arrow" /></span
      ></RouterLink>
    </div>
    <p v-if="bootstrap && !bootstrap.apps.length" class="empty">
      {{ t("No applications are available.") }}
    </p></template
  >
</template>
