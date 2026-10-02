<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watchEffect } from "vue";
import { t } from "./i18n";
import AppShell from "./components/AppShell.vue";
import InstallCommands from "./components/InstallCommands.vue";
import Icon from "./components/Icon.vue";
import { siteTitle } from "./site";
type Application = {
  id: string;
  name: string;
  summary: string;
  origin: string;
  icon: string;
};
const applications = ref<Application[]>([]),
  loading = ref(true),
  error = ref(false);
const detail = window.location.pathname === "/apps/codex";
const selected = computed(() =>
  applications.value.find((app) => app.id === "codex"),
);
let controller: AbortController | undefined,
  disposed = false;
async function load() {
  if (controller) return;
  controller = new AbortController();
  loading.value = true;
  error.value = false;
  try {
    const response = await fetch("/api/apps", {
      credentials: "omit",
      cache: "no-store",
      signal: controller.signal,
    });
    if (!response.ok) throw Error();
    const data = await response.json();
    if (!disposed) applications.value = data;
  } catch (reason) {
    if (!disposed && !(reason instanceof Error && reason.name === "AbortError"))
      error.value = true;
  } finally {
    controller = undefined;
    loading.value = false;
  }
}
watchEffect(() => {
  document.title = `${detail ? t("Install Codex CLI") : t("Applications")} · ${siteTitle.value}`;
});
onMounted(load);
onUnmounted(() => {
  disposed = true;
  controller?.abort();
});
</script>
<template>
  <AppShell
    ><template #actions
      ><a href="/admin/" class="header-link admin-link"
        ><Icon name="user" /><span>{{ t("Administrator") }}</span></a
      ></template
    >
    <main id="main-content" class="public-main" tabindex="-1">
      <div v-if="loading" class="empty panel" role="status">
        {{ t("Loading applications…") }}
      </div>
      <section v-else-if="error" class="panel empty-state" role="alert">
        <Icon name="alert" :size="28" />
        <h1>{{ t("Unable to load applications") }}</h1>
        <p class="muted">
          {{ t("Connection failed. Check your connection and retry.") }}
        </p>
        <button @click="load">{{ t("Retry") }}</button>
      </section>
      <template v-else-if="detail && selected">
        <a href="/" class="back-link"
          ><Icon name="back" :size="16" />{{ t("All applications") }}</a
        >
        <div class="application-identity">
          <div class="application-logo">
            <img
              :src="selected.icon"
              :alt="t('OpenAI brand mark')"
              width="48"
              height="48"
            />
          </div>
          <div class="public-heading">
            <span class="eyebrow">{{ t("Installation instructions") }}</span>
            <h1>{{ selected.name }}</h1>
            <p class="public-lead">
              {{ t("OpenAI’s coding agent for your terminal.") }}
            </p>
          </div>
        </div>
        <div class="installation-layout">
          <InstallCommands :origin="selected.origin" />
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
                <li>
                  {{
                    t(
                      "Start codex and follow your organization’s sign-in instructions.",
                    )
                  }}
                </li>
              </ol>
            </section>
          </aside>
        </div>
      </template>
      <template v-else
        ><div class="public-heading">
          <h1>{{ t("Applications") }}</h1>
          <p class="public-lead">
            {{ t("Install tools from your organization’s download service.") }}
          </p>
        </div>
        <div class="application-list">
          <a
            v-for="app in applications"
            :key="app.id"
            :href="`/apps/${app.id}`"
            class="application-card"
            ><div class="application-card-brand">
              <div class="application-logo">
                <img
                  :src="app.icon"
                  :alt="t('OpenAI brand mark')"
                  width="40"
                  height="40"
                />
              </div>
              <span class="app-publisher">OpenAI</span>
            </div>
            <h2>{{ app.name }}</h2>
            <p>{{ t("OpenAI’s coding agent for your terminal.") }}</p>
            <span class="card-action"
              >{{ t("Installation instructions") }}<Icon name="arrow" /></span
          ></a>
        </div>
        <p v-if="!applications.length" class="empty">
          {{ t("No applications are available.") }}
        </p></template
      >
    </main></AppShell
  >
</template>
