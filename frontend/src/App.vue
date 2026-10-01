<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { api, ApiError, setCSRF, type Metric, type Status } from "./api";
import InstallCommands from "./components/InstallCommands.vue";
import Overview from "./components/Overview.vue";
import Resources from "./components/Resources.vue";
import Events from "./components/Events.vue";
import Maintenance from "./components/Maintenance.vue";
import ProxySettings from "./components/ProxySettings.vue";
import HistoryDialog from "./components/HistoryDialog.vue";
const activeMetric = ref<Metric>();
const status = ref<Status>(),
  signedIn = ref(false),
  password = ref(""),
  loading = ref(false),
  error = ref(""),
  notice = ref(""),
  tab = ref("Overview"),
  automatic = ref(true);
let timer: ReturnType<typeof setTimeout> | undefined,
  controller: AbortController | undefined,
  disposed = false;
const tabs = ["Overview", "Versions", "Events", "Settings"];
const title = computed(() =>
  tab.value === "Overview" ? "Distribution overview" : tab.value,
);
function stop() {
  clearTimeout(timer);
  controller?.abort();
  controller = undefined;
}
function expire(message = "Your session expired. Sign in again.") {
  stop();
  setCSRF("");
  signedIn.value = false;
  status.value = undefined;
  notice.value = message;
  password.value = "";
  activeMetric.value = undefined;
}
function failed(reason: unknown) {
  if (reason instanceof ApiError && reason.status === 401) {
    expire();
    return;
  }
  if (reason instanceof Error && reason.name === "AbortError") return;
  error.value = reason instanceof Error ? reason.message : "Request failed";
}
function schedule() {
  clearTimeout(timer);
  if (!disposed && signedIn.value && automatic.value)
    timer = setTimeout(() => void refresh(), 5000);
}
async function refresh() {
  if (loading.value) return;
  loading.value = true;
  error.value = "";
  controller = new AbortController();
  try {
    status.value = await api<Status>("status", undefined, controller.signal);
  } catch (reason) {
    failed(reason);
  } finally {
    loading.value = false;
    controller = undefined;
    schedule();
  }
}
async function login() {
  loading.value = true;
  error.value = "";
  try {
    const session = await api<{ csrf: string }>("login", {
      password: password.value,
    });
    setCSRF(session.csrf);
    password.value = "";
    signedIn.value = true;
    notice.value = "";
    loading.value = false;
    await refresh();
  } catch (reason) {
    failed(reason);
  } finally {
    loading.value = false;
  }
}
async function logout() {
  try {
    await api("logout", {});
    expire("Signed out");
  } catch (reason) {
    failed(reason);
  }
}
function polling() {
  if (automatic.value) schedule();
  else clearTimeout(timer);
}
onMounted(async () => {
  loading.value = true;
  try {
    const session = await api<{ csrf: string }>("session");
    if (disposed) return;
    setCSRF(session.csrf);
    signedIn.value = true;
    loading.value = false;
    await refresh();
  } catch (reason) {
    if (!(reason instanceof ApiError && reason.status === 401)) failed(reason);
  } finally {
    loading.value = false;
  }
});
onUnmounted(() => {
  disposed = true;
  stop();
});
</script>
<template>
  <div class="app-shell">
    <header>
      <a class="brand" href="/admin/">RedApp<span>Codex distribution</span></a>
      <div v-if="signedIn" class="header-actions">
        <label
          ><input v-model="automatic" type="checkbox" @change="polling" /> Auto
          refresh</label
        ><button class="secondary" @click="refresh" :disabled="loading">
          {{ loading ? "Refreshing…" : "Refresh" }}</button
        ><button class="secondary" @click="logout">Sign out</button>
      </div>
    </header>
    <main>
      <p v-if="notice" class="notice" role="status">{{ notice }}</p>
      <div v-if="error" class="error" role="alert">
        {{ error }}
        <button v-if="signedIn" @click="refresh" :disabled="loading">
          Retry
        </button>
      </div>
      <section v-if="!signedIn" class="login">
        <span class="eyebrow">Administrator access</span>
        <h1>Sign in to RedApp</h1>
        <p class="muted">
          Use the password from the first initialization logs. Change it after
          signing in.
        </p>
        <form @submit.prevent="login">
          <label
            >Password<input
              v-model="password"
              type="password"
              autocomplete="current-password"
              required /></label
          ><button :disabled="loading">
            {{ loading ? "Signing in…" : "Sign in" }}
          </button>
        </form>
      </section>
      <template v-else
        ><nav aria-label="Admin sections">
          <button
            v-for="item in tabs"
            :key="item"
            :aria-current="tab === item ? 'page' : undefined"
            @click="tab = item"
          >
            {{ item }}
          </button>
        </nav>
        <div class="page-heading">
          <div>
            <span class="eyebrow">Enterprise distribution</span>
            <h1>{{ title }}</h1>
          </div>
          <span v-if="status" class="muted"
            >{{ status.os }} / {{ status.arch }}</span
          >
        </div>
        <p v-if="!status" role="status">
          {{
            loading
              ? "Loading service status…"
              : "Status unavailable. Retry to reconnect."
          }}
        </p>
        <template v-if="status"
          ><template v-if="tab === 'Overview'"
            ><Overview
              :status="status"
              @history="activeMetric = $event" /><InstallCommands
              :origin="status.public_base_url" /></template
          ><Resources v-if="tab === 'Versions'" :status="status" /><Events
            v-if="tab === 'Events'"
            :events="status.events || []"
          /><ProxySettings
            v-if="tab === 'Settings'"
            @error="failed"
          /><Maintenance
            v-if="tab === 'Settings'"
            @error="failed"
            @expired="expire('Password changed. Sign in again.')"
            @changed="refresh"
          />
          <footer>{{ status.go }} · {{ status.sampled_at }}</footer></template
        ></template
      >
    </main>
    <HistoryDialog
      v-if="activeMetric && signedIn"
      :metric="activeMetric"
      @close="activeMetric = undefined"
      @error="failed"
    />
  </div>
</template>
