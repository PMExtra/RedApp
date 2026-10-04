<script setup lang="ts">
import { ref, computed } from "vue";
import { useSetting } from "../composables/useSetting";
import { t, errorText } from "../i18n";
const { draft, loading, saving, error, saved, load, save } = useSetting<{
  keys: string[];
}>(ref("settings/homepage"));
const input = ref("");
const busy = computed(() => loading.value || saving.value);
function add() {
  const key = input.value.trim();
  if (
    draft.value &&
    /^[a-z0-9-]+\/[a-z0-9-]+$/.test(key) &&
    !draft.value.keys.includes(key) &&
    draft.value.keys.length < 100
  ) {
    draft.value.keys.push(key);
    input.value = "";
  }
}
function move(index: number, delta: number) {
  if (!draft.value) return;
  const next = index + delta;
  if (next < 0 || next >= draft.value.keys.length) return;
  const [item] = draft.value.keys.splice(index, 1);
  draft.value.keys.splice(next, 0, item!);
}
</script>
<template>
  <section class="panel">
    <h2>{{ t("Pinned applications") }}</h2>
    <p class="muted">
      {{
        t(
          "Choose application keys in display order. Disabled applications remain saved here and are hidden publicly.",
        )
      }}
    </p>
    <p v-if="error" class="error" role="alert">{{ errorText(error) }}</p>
    <p v-if="saved" class="notice" role="status">{{ t("Changes saved.") }}</p>
    <form v-if="draft" @submit.prevent="save">
      <fieldset :disabled="busy">
        <ol class="pinned-order">
          <li v-for="(key, index) in draft.keys" :key="key">
            <code>{{ key }}</code>
            <div class="form-actions">
              <button
                type="button"
                class="secondary"
                :disabled="index === 0"
                :aria-label="`${t('Move up')}: ${key}`"
                @click="move(index, -1)"
              >
                {{ t("Move up") }}</button
              ><button
                type="button"
                class="secondary"
                :disabled="index === draft.keys.length - 1"
                :aria-label="`${t('Move down')}: ${key}`"
                @click="move(index, 1)"
              >
                {{ t("Move down") }}</button
              ><button
                type="button"
                class="secondary"
                :aria-label="`${t('Remove')}: ${key}`"
                @click="draft.keys.splice(index, 1)"
              >
                {{ t("Remove") }}
              </button>
            </div>
          </li>
        </ol>
        <div class="form-actions">
          <input
            v-model="input"
            :aria-label="t('Application key')"
            placeholder="vendor/application"
            spellcheck="false"
            @keydown.enter.prevent="add"
          /><button
            type="button"
            class="secondary"
            :disabled="!input.trim() || draft.keys.length >= 100"
            @click="add"
          >
            {{ t("Add application") }}
          </button>
        </div>
      </fieldset>
      <div class="form-actions">
        <button :disabled="busy">
          {{ saving ? t("Saving…") : t("Save changes") }}</button
        ><button
          type="button"
          class="secondary"
          :disabled="busy"
          @click="load()"
        >
          {{ t("Reload") }}
        </button>
      </div>
    </form>
    <p v-else-if="loading" role="status">{{ t("Loading…") }}</p>
  </section>
</template>
