<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { api, putSetting, type ProxySettings } from "../api";
import { errorText, t } from "../i18n";
import SelectMenu from "./SelectMenu.vue";
import { useDirtyDraft } from "../composables/useDirtyDraft";
const emit = defineEmits<{ error: [unknown] }>();
const error = ref<unknown>();
const revision = ref<number>();
const server = ref(""),
  username = ref(""),
  password = ref(""),
  action = ref("keep"),
  saved = ref<ProxySettings>(),
  busy = ref(false),
  message = ref(false),
  loading = ref(false);
const dirty = computed(
  () =>
    !!saved.value &&
    (server.value !== saved.value.server || action.value !== "keep"),
);
const confirmDiscard = useDirtyDraft(dirty);
let controller: AbortController | undefined,
  disposed = false;
watch(action, (value) => {
  if (value !== "replace") {
    username.value = "";
    password.value = "";
  }
});
async function load() {
  if (busy.value || !confirmDiscard()) return;
  busy.value = true;
  error.value = undefined;
  loading.value = true;
  message.value = false;
  error.value = undefined;
  controller = new AbortController();
  try {
    const data = await api<ProxySettings & { revision: number }>(
      "settings/proxy",
      undefined,
      controller.signal,
    );
    if (!disposed) {
      revision.value = data.revision;
      saved.value = data;
      server.value = data.server;
      username.value = "";
      password.value = "";
      action.value = "keep";
    }
  } catch (reason) {
    if (!disposed) {
      error.value = reason;
      emit("error", reason);
    }
  } finally {
    busy.value = false;
    loading.value = false;
    controller = undefined;
  }
}
async function save() {
  if (busy.value || !saved.value || revision.value === undefined) return;
  busy.value = true;
  message.value = false;
  error.value = undefined;
  controller = new AbortController();
  try {
    const data = await putSetting<ProxySettings & { revision: number }>(
      "settings/proxy",
      {
        server: server.value,
        username: action.value === "replace" ? username.value : "",
        password: action.value === "replace" ? password.value : "",
        password_action: action.value,
      },
      revision.value,
      controller.signal,
    );
    if (!disposed) {
      revision.value = data.revision;
      saved.value = data;
      server.value = data.server;
      username.value = "";
      password.value = "";
      action.value = "keep";
      message.value = true;
    }
  } catch (reason) {
    if (!disposed) {
      error.value = reason;
      emit("error", reason);
    }
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
    <p v-if="error" class="error" role="alert">{{ errorText(error) }}</p>
    <form @submit.prevent="save">
      <fieldset :disabled="busy || !saved">
        <div class="two-columns">
          <label
            >{{ t("Proxy server")
            }}<input
              v-model="server"
              placeholder="http://proxy.example:3128"
              autocomplete="off"
          /></label>
          <div class="select-field">
            <span>{{ t("Saved credentials") }}</span
            ><SelectMenu
              v-model="action"
              :disabled="busy || !saved"
              :label="t('Saved credentials')"
              :options="[
                { value: 'keep', label: t('Keep saved credentials') },
                { value: 'replace', label: t('Replace credentials') },
                { value: 'clear', label: t('Clear credentials') },
              ]"
            />
          </div>
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
