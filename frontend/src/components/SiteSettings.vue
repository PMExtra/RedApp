<script setup lang="ts">
import { computed } from "vue";
import { t, errorText } from "../i18n";
import { applySite, type SiteSettings } from "../site";
import { invalidateBootstrap } from "../bootstrap";
import { useSetting } from "../composables/useSetting";
const { draft, loading, saving, saved, error, load, save } =
  useSetting<SiteSettings>(
    computed(() => "settings/site"),
    (value, saved) => {
      if (saved) invalidateBootstrap();
      applySite(value);
    },
  );
const busy = computed(() => loading.value || saving.value);
const loaded = computed(() => !!draft.value);
const languages = ["en", "zh-CN"] as const;
function request(saving = false) {
  return saving ? save() : load();
}
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
    <p v-if="error" class="error" role="alert">{{ errorText(error) }}</p>
    <p v-if="loading" role="status">{{ t("Loading…") }}</p>
    <form @submit.prevent="request(true)">
      <fieldset v-if="draft" :disabled="busy || !loaded">
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
