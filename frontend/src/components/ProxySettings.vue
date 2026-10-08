<script setup lang="ts">
import ProxySection from "./ProxySection.vue";
import type { ProxyConfig } from "../configuration";
import IconButton from "./IconButton.vue";
import { computed, onMounted, onUnmounted, ref } from "vue";
import { api, isCancellation, putSetting, type ProxySettings } from "../api";
import { errorText, t } from "../i18n";
import { useDirtyDraft } from "../composables/useDirtyDraft";
const emit = defineEmits<{ error: [unknown] }>();
const error = ref<unknown>();
const revision = ref<number>();
const proxy = ref<ProxyConfig>({ mode: "direct" });
const server = ref(""),
  saved = ref<ProxySettings>(),
  busy = ref(false),
  message = ref(false),
  loading = ref(false);
const dirty = computed(
  () =>
    !!saved.value &&
    JSON.stringify(proxy.value) !==
      JSON.stringify(
        saved.value.mode
          ? {
              mode: saved.value.mode,
              ...(saved.value.mode === "url"
                ? { url: saved.value.url || saved.value.server }
                : {}),
            }
          : {
              mode: saved.value.server ? "url" : "direct",
              ...(saved.value.server ? { url: saved.value.server } : {}),
            },
      ),
);
const confirmDiscard = useDirtyDraft(dirty);
let controller: AbortController | undefined,
  disposed = false;
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
      proxy.value = data.mode
        ? {
            mode: data.mode,
            ...(data.mode === "url" ? { url: data.url || data.server } : {}),
          }
        : {
            mode: data.server ? "url" : "direct",
            ...(data.server ? { url: data.server } : {}),
          };
    }
  } catch (reason) {
    if (!disposed && !isCancellation(reason)) {
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
  if (
    busy.value ||
    !saved.value ||
    revision.value === undefined ||
    !dirty.value
  )
    return;
  busy.value = true;
  message.value = false;
  error.value = undefined;
  controller = new AbortController();
  try {
    const data = await putSetting<ProxySettings & { revision: number }>(
      "settings/proxy",
      proxy.value,
      revision.value,
      controller.signal,
    );
    if (!disposed) {
      revision.value = data.revision;
      saved.value = data;
      server.value = data.server;
      proxy.value = data.mode
        ? {
            mode: data.mode,
            ...(data.mode === "url" ? { url: data.url || data.server } : {}),
          }
        : {
            mode: data.server ? "url" : "direct",
            ...(data.server ? { url: data.server } : {}),
          };
      message.value = true;
    }
  } catch (reason) {
    if (!disposed && !isCancellation(reason)) {
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
});
</script>
<template>
  <section class="panel">
    <h2>{{ t("Upstream proxy") }}</h2>
    <p class="muted">
      {{
        t(
          "HTTP / HTTPS / SOCKS5 proxy for metadata, artifacts and imports. Select Direct connection to disable the proxy. Environment proxy variables are ignored; SOCKS5 uses proxy-side DNS.",
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
        <ProxySection v-model="proxy" global :disabled="busy" />
      </fieldset>
      <p class="muted small-text">
        {{
          t(
            "Enter one complete URL with percent-encoded credentials. The saved URL is visible to administrators.",
          )
        }}
      </p>
      <div class="form-actions">
        <button :disabled="busy || !saved">
          {{ busy && !loading ? t("Saving…") : t("Save proxy") }}</button
        ><IconButton
          class="secondary"
          type="button"
          :disabled="busy"
          @click="load"
          icon="refresh"
          :label="t('Reload')"
        />
      </div>
    </form>
  </section>
</template>
