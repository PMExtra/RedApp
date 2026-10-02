<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watchEffect } from "vue";
import { api, ApiError, setCSRF, type Metric, type Status } from "./api";
import { errorText, localDate, t, type Message } from "./i18n";
import AppShell from "./components/AppShell.vue";
import AccountMenu from "./components/AccountMenu.vue";
import PasswordDialog from "./components/PasswordDialog.vue";
import Icon from "./components/Icon.vue";
import Overview from "./components/Overview.vue";
import Resources from "./components/Resources.vue";
import Events from "./components/Events.vue";
import Maintenance from "./components/Maintenance.vue";
import ProxySettings from "./components/ProxySettings.vue";
import HistoryDialog from "./components/HistoryDialog.vue";
const activeMetric = ref<Metric>(),
  status = ref<Status>(),
  signedIn = ref(false),
  password = ref(""),
  checking = ref(true),
  loading = ref(false),
  authBusy = ref(false),
  error = ref<unknown>(),
  notice = ref<Message>(),
  tab = ref("Overview"),
  automatic = ref(true),
  passwordOpen = ref(false);
const tabs: { name: Message; icon: string; description: Message }[] = [
  {
    name: "Overview",
    icon: "grid",
    description: "Monitor downloads, storage and service activity.",
  },
  {
    name: "Versions",
    icon: "box",
    description: "Inspect cached versions and active resource generations.",
  },
  {
    name: "Events",
    icon: "activity",
    description: "Review recent download and upstream failures.",
  },
  {
    name: "Settings",
    icon: "settings",
    description: "Configure upstream access and maintain your cache.",
  },
];
const current = computed(() => tabs.find((item) => item.name === tab.value)!);
let timer: ReturnType<typeof setTimeout> | undefined,
  controller: AbortController | undefined,
  authController: AbortController | undefined,
  disposed = false,
  epoch = 0;
function stop() {
  clearTimeout(timer);
  controller?.abort();
  controller = undefined;
  loading.value = false;
  epoch++;
}
function expire(message: Message = "Your session expired. Sign in again.") {
  stop();
  setCSRF("");
  signedIn.value = false;
  status.value = undefined;
  notice.value = message;
  password.value = "";
  activeMetric.value = undefined;
  passwordOpen.value = false;
  error.value = undefined;
}
function failed(reason: unknown) {
  if (reason instanceof ApiError && reason.status === 401) {
    expire();
    return;
  }
  if (reason instanceof Error && reason.name === "AbortError") return;
  error.value = reason;
}
function schedule() {
  clearTimeout(timer);
  if (!disposed && signedIn.value && automatic.value && !authBusy.value)
    timer = setTimeout(() => void refresh(), 5000);
}
async function refresh() {
  if (loading.value || disposed || !signedIn.value) return;
  clearTimeout(timer);
  loading.value = true;
  error.value = undefined;
  controller = new AbortController();
  const ticket = epoch;
  try {
    const next = await api<Status>("status", undefined, controller.signal);
    if (!disposed && ticket === epoch) status.value = next;
  } catch (reason) {
    if (!disposed && ticket === epoch) failed(reason);
  } finally {
    if (ticket === epoch) {
      loading.value = false;
      controller = undefined;
      schedule();
    }
  }
}
async function login() {
  if (authBusy.value || checking.value || signedIn.value) return;
  authBusy.value = true;
  error.value = undefined;
  authController = new AbortController();
  try {
    const session = await api<{ csrf: string }>(
      "login",
      { password: password.value },
      authController.signal,
    );
    if (disposed) return;
    setCSRF(session.csrf);
    password.value = "";
    signedIn.value = true;
    notice.value = undefined;
    authBusy.value = false;
    await refresh();
  } catch (reason) {
    if (!disposed) failed(reason);
  } finally {
    authBusy.value = false;
  }
}
async function logout() {
  if (authBusy.value) return;
  authBusy.value = true;
  stop();
  authController = new AbortController();
  try {
    await api("logout", {}, authController.signal);
    if (!disposed) expire("Signed out");
  } catch (reason) {
    if (!disposed) failed(reason);
  } finally {
    authBusy.value = false;
    schedule();
  }
}
function polling() {
  automatic.value = !automatic.value;
  if (automatic.value) schedule();
  else clearTimeout(timer);
}
watchEffect(() => {
  document.title = `${t("Administration")} · RedApp`;
});
onMounted(async () => {
  authController = new AbortController();
  try {
    const session = await api<{ csrf: string }>(
      "session",
      undefined,
      authController.signal,
    );
    if (disposed) return;
    setCSRF(session.csrf);
    signedIn.value = true;
    checking.value = false;
    await refresh();
  } catch (reason) {
    if (!disposed && !(reason instanceof ApiError && reason.status === 401))
      failed(reason);
  } finally {
    checking.value = false;
  }
});
onUnmounted(() => {
  disposed = true;
  stop();
  authController?.abort();
  password.value = "";
});
</script>
<template>
  <AppShell>
    <template #actions
      ><AccountMenu
        v-if="signedIn"
        :busy="authBusy"
        @password="passwordOpen = true"
        @logout="logout" /><a v-else class="header-link" href="/"
        >{{ t("Applications") }}<Icon name="arrow" :size="16" /></a
    ></template>
    <div :class="['admin-layout', { 'is-signed-out': !signedIn }]">
      <aside v-if="signedIn" class="sidebar">
        <span class="eyebrow">{{ t("Administration") }}</span>
        <nav :aria-label="t('Administration')">
          <button
            v-for="item in tabs"
            :key="item.name"
            :aria-current="tab === item.name ? 'page' : undefined"
            @click="tab = item.name"
          >
            <Icon :name="item.icon" /><span>{{ t(item.name) }}</span>
          </button>
        </nav>
        <a href="/" class="sidebar-public"
          ><Icon name="arrow" :size="16" />{{
            t("Public installation page")
          }}</a
        >
      </aside>
      <main
        id="main-content"
        :class="['admin-main', { 'auth-main': !signedIn }]"
        tabindex="-1"
      >
        <p v-if="notice" class="notice" role="status">{{ t(notice) }}</p>
        <div v-if="error" class="error" role="alert">
          <Icon name="alert" />
          <div>
            {{ errorText(error)
            }}<small v-if="status">{{
              t("Showing the last successful snapshot.")
            }}</small>
          </div>
          <button
            v-if="signedIn"
            class="secondary"
            @click="refresh"
            :disabled="loading"
          >
            {{ t("Retry") }}
          </button>
        </div>
        <section v-if="!signedIn" class="login panel">
          <div class="login-mark"><Icon name="lock" :size="24" /></div>
          <span class="eyebrow">{{ t("Administrator access") }}</span>
          <h1>{{ t("Sign in to RedApp") }}</h1>
          <p class="muted">
            {{
              t(
                "Use the password from the first initialization logs. Change it after signing in.",
              )
            }}
          </p>
          <form @submit.prevent="login">
            <label
              >{{ t("Password")
              }}<input
                v-model="password"
                type="password"
                autocomplete="current-password"
                :disabled="checking || authBusy"
                required /></label
            ><button :disabled="checking || authBusy">
              {{
                checking
                  ? t("Loading…")
                  : authBusy
                    ? t("Signing in…")
                    : t("Sign in")
              }}<Icon name="arrow" />
            </button>
          </form>
        </section>
        <template v-else>
          <div class="page-heading">
            <div>
              <h1>
                {{
                  tab === "Overview"
                    ? t("Distribution overview")
                    : t(current.name)
                }}
              </h1>
              <p class="muted">{{ t(current.description) }}</p>
            </div>
          </div>
          <div class="snapshot-toolbar">
            <span class="snapshot"
              ><Icon name="clock" :size="16" />{{ t("Snapshot") }}
              <time
                v-if="status"
                :datetime="status.sampled_at"
                :title="t('Local time')"
                >{{ localDate(status.sampled_at) }}</time
              ><span v-else>—</span></span
            >
            <div class="refresh-actions">
              <button
                class="secondary auto-refresh"
                :aria-pressed="automatic"
                :title="t('Refresh every 5 seconds')"
                @click="polling"
              >
                <Icon name="refresh" /><span>{{ t("Auto refresh") }}</span
                ><span class="toggle-state">{{
                  automatic ? t("On") : t("Off")
                }}</span></button
              ><button class="secondary" @click="refresh" :disabled="loading">
                <Icon
                  name="refresh"
                  :class="{ 'is-spinning': loading }"
                /><span>{{ loading ? t("Refreshing…") : t("Refresh") }}</span>
              </button>
            </div>
          </div>
          <div v-if="!status" class="empty panel" role="status">
            {{
              loading
                ? t("Loading service status…")
                : t("Status unavailable. Retry to reconnect.")
            }}
          </div>
          <template v-else
            ><Overview
              v-if="tab === 'Overview'"
              :status="status"
              @history="activeMetric = $event" /><Resources
              v-if="tab === 'Versions'"
              :status="status" /><Events
              v-if="tab === 'Events'"
              :events="status.events || []" />
            <div v-if="tab === 'Settings'" class="settings-stack">
              <Maintenance @error="failed" @changed="refresh" /><ProxySettings
                @error="failed"
              /></div
          ></template>
        </template>
      </main>
    </div>
  </AppShell>
  <HistoryDialog
    v-if="activeMetric && signedIn"
    :metric="activeMetric"
    @close="activeMetric = undefined"
    @error="failed"
  />
  <PasswordDialog
    v-if="passwordOpen && signedIn"
    @close="passwordOpen = false"
    @changed="expire('Password changed. Sign in again.')"
    @error="failed"
  />
</template>
