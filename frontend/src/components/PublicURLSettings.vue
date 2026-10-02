<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { api, putSetting } from "../api";
import { applyPublicOrigin } from "../bootstrap";
import { useDirtyDraft } from "../composables/useDirtyDraft";
import { errorText, t } from "../i18n";
interface PublicURL {
  override_url: string | null;
  environment_url: string | null;
  effective_url: string;
  source: "override" | "environment" | "request";
  revision: number;
}
const value = ref<PublicURL>(),
  draft = ref(""),
  busy = ref(false),
  saved = ref(false),
  error = ref<unknown>();
let controller: AbortController | undefined,
  disposed = false;
const dirty = computed(
  () => !!value.value && draft.value !== (value.value.override_url || ""),
);
const confirmDiscard = useDirtyDraft(dirty);
const source = computed(() =>
  value.value?.source === "override"
    ? t("Administrator override")
    : value.value?.source === "environment"
      ? t("Environment")
      : t("Request origin"),
);
async function request(save = false) {
  if (busy.value || (save && !value.value) || (!save && !confirmDiscard()))
    return;
  busy.value = true;
  error.value = undefined;
  saved.value = false;
  controller = new AbortController();
  try {
    const next = save
      ? await putSetting<PublicURL>(
          "settings/public-url",
          { override_url: draft.value.trim() || null },
          value.value!.revision,
          controller.signal,
        )
      : await api<PublicURL>(
          "settings/public-url",
          undefined,
          controller.signal,
        );
    if (!disposed) {
      value.value = next;
      draft.value = next.override_url || "";
      saved.value = save;
      if (save) applyPublicOrigin(next.effective_url);
    }
  } catch (reason) {
    if (!disposed) error.value = reason;
  } finally {
    busy.value = false;
    controller = undefined;
  }
}
onMounted(() => request());
onUnmounted(() => {
  disposed = true;
  controller?.abort();
});
</script>
<template>
  <section class="panel public-url-settings">
    <h2>{{ t("Public URL") }}</h2>
    <p v-if="error" class="error" role="alert">{{ errorText(error) }}</p>
    <p v-if="saved" class="notice" role="status">
      {{ t("Public URL saved.") }}
    </p>
    <dl v-if="value" class="public-url-effective">
      <dt>{{ t("Effective URL") }}</dt>
      <dd>
        <code>{{ value.effective_url }}</code>
      </dd>
      <dt>{{ t("Configuration source") }}</dt>
      <dd>{{ source }}</dd>
      <dt>{{ t("Environment URL") }}</dt>
      <dd>{{ value.environment_url || t("No environment URL") }}</dd>
    </dl>
    <form @submit.prevent="request(true)">
      <label
        >{{ t("Public URL override")
        }}<input
          v-model="draft"
          type="url"
          placeholder="https://downloads.example.com"
          :disabled="busy || !value"
      /></label>
      <p class="muted">
        {{
          t(
            "Leave empty to use the environment URL or the safe request origin.",
          )
        }}
      </p>
      <div class="form-actions">
        <button :disabled="busy || !value">
          {{ busy ? t("Saving…") : t("Save") }}</button
        ><button
          type="button"
          class="secondary"
          :disabled="busy || !value"
          @click="draft = ''"
        >
          {{ t("Clear override") }}</button
        ><button
          type="button"
          class="secondary"
          :disabled="busy"
          @click="request()"
        >
          {{ t("Reload") }}
        </button>
      </div>
    </form>
  </section>
</template>
