<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from "vue";
import { api, type ProxySettings } from "../api";
import { t } from "../i18n";
const emit = defineEmits<{ error: [unknown] }>();
const server = ref(""),
  username = ref(""),
  password = ref(""),
  action = ref("keep"),
  saved = ref<ProxySettings>(),
  busy = ref(false),
  message = ref(false),
  loading = ref(false);
let controller: AbortController | undefined,
  disposed = false;
watch(action, (value) => {
  if (value !== "replace") {
    username.value = "";
    password.value = "";
  }
});
async function load() {
  if (busy.value) return;
  busy.value = true;
  loading.value = true;
  message.value = false;
  controller = new AbortController();
  try {
    const data = await api<ProxySettings>(
      "proxy",
      undefined,
      controller.signal,
    );
    if (!disposed) {
      saved.value = data;
      server.value = data.server;
      username.value = "";
      password.value = "";
      action.value = "keep";
    }
  } catch (error) {
    if (!disposed) emit("error", error);
  } finally {
    busy.value = false;
    loading.value = false;
    controller = undefined;
  }
}
async function save() {
  if (busy.value || !saved.value) return;
  busy.value = true;
  message.value = false;
  controller = new AbortController();
  try {
    const data = await api<ProxySettings>(
      "proxy",
      {
        server: server.value,
        username: action.value === "replace" ? username.value : "",
        password: action.value === "replace" ? password.value : "",
        password_action: action.value,
      },
      controller.signal,
    );
    if (!disposed) {
      saved.value = data;
      server.value = data.server;
      username.value = "";
      password.value = "";
      action.value = "keep";
      message.value = true;
    }
  } catch (error) {
    if (!disposed) emit("error", error);
  } finally {
    busy.value = false;
    controller = undefined;
  }
}
onMounted(load);
onUnmounted(() => {
  disposed = true;
  controller?.abort();
  username.value = "";
  password.value = "";
});
</script>
<template>
  <section class="panel">
    <h2>{{ t("Upstream proxy") }}</h2>
    <p class="muted">
      {{
        t(
          "Optional HTTP / HTTPS / SOCKS5 proxy for metadata, artifacts and resume requests. Empty means direct; environment proxy variables are ignored. SOCKS5 uses proxy-side DNS; the proxy must be trusted.",
        )
      }}
    </p>
    <p v-if="message" class="notice" role="status">
      {{
        t(
          "Proxy saved; new upstream requests use this configuration. Active transfers continue.",
        )
      }}
    </p>
    <p v-if="loading" role="status">{{ t("Loading proxy settings…") }}</p>
    <form @submit.prevent="save">
      <fieldset :disabled="busy || !saved">
        <div class="two-columns">
          <label
            >{{ t("Proxy server")
            }}<input
              v-model="server"
              placeholder="http://proxy.example:3128"
              autocomplete="off" /></label
          ><label
            >{{ t("Saved credentials")
            }}<select v-model="action">
              <option value="keep">{{ t("Keep saved credentials") }}</option>
              <option value="replace">{{ t("Replace credentials") }}</option>
              <option value="clear">{{ t("Clear credentials") }}</option>
            </select></label
          >
        </div>
        <p class="muted small-text">
          {{
            saved?.has_credentials
              ? t("Credentials saved (never displayed)")
              : t("No saved credentials")
          }}
        </p>
        <div v-if="action === 'replace'" class="two-columns">
          <label
            >{{ t("Username")
            }}<input v-model="username" autocomplete="off" /></label
          ><label
            >{{ t("Password")
            }}<input
              v-model="password"
              type="password"
              autocomplete="new-password"
          /></label>
        </div>
      </fieldset>
      <p class="muted small-text">
        {{
          t(
            "Changing the server requires clearing or replacing saved credentials. Credentials are stored in the local SQLite database under data-directory permissions; no extra storage encryption is applied.",
          )
        }}
      </p>
      <div class="form-actions">
        <button :disabled="busy || !saved">
          {{ busy && !loading ? t("Saving…") : t("Save proxy") }}</button
        ><button class="secondary" type="button" :disabled="busy" @click="load">
          {{ t("Reload") }}
        </button>
      </div>
    </form>
  </section>
</template>
