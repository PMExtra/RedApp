<script setup lang="ts">
import { onMounted, onUnmounted, ref } from "vue";
import { api } from "../api";
import { t } from "../i18n";
import { applySite, defaultSite, type SiteSettings } from "../site";
const emit = defineEmits<{ error: [unknown] }>();
const draft = ref<SiteSettings>(structuredClone(defaultSite)),
  busy = ref(false),
  loaded = ref(false),
  saved = ref(false);
const languages = ["en", "zh-CN"] as const;
let controller: AbortController | undefined,
  disposed = false;
async function request(save = false) {
  if (busy.value) return;
  busy.value = true;
  saved.value = false;
  controller = new AbortController();
  try {
    const value = await api<SiteSettings>(
      "site",
      save ? draft.value : undefined,
      controller.signal,
    );
    if (!disposed) {
      draft.value = value;
      loaded.value = true;
      applySite(value);
      saved.value = save;
    }
  } catch (error) {
    if (!disposed) emit("error", error);
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
  <section class="panel site-settings">
    <h2>{{ t("Site appearance") }}</h2>
    <p class="muted">
      {{
        t(
          "Public text for each language. Plain text only; the project link always points to RedApp.",
        )
      }}
    </p>
    <p v-if="saved" class="notice" role="status">
      {{ t("Site settings saved.") }}
    </p>
    <form @submit.prevent="request(true)">
      <fieldset :disabled="busy || !loaded">
        <div class="two-columns">
          <fieldset v-for="lang in languages" :key="lang" class="site-locale">
            <legend>{{ lang === "en" ? "English" : "简体中文" }}</legend>
            <label
              >{{ t("Site title")
              }}<input
                v-model="draft.title[lang]"
                required
                maxlength="80"
                :lang="lang"
            /></label>
            <label
              >{{ t("Site subtitle")
              }}<input
                v-model="draft.subtitle[lang]"
                maxlength="160"
                :lang="lang"
            /></label>
            <label
              >{{ t("Footer notice")
              }}<textarea
                v-model="draft.disclaimer[lang]"
                rows="4"
                maxlength="500"
                :lang="lang"
              />
            </label>
          </fieldset>
        </div>
      </fieldset>
      <div class="form-actions">
        <button :disabled="busy || !loaded">
          {{ busy ? t("Saving…") : t("Save site settings") }}</button
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
