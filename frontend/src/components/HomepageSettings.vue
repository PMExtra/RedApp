<script setup lang="ts">
import IconButton from "./IconButton.vue";
import SortableList from "./SortableList.vue";
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
        <SortableList
          v-model="draft.keys"
          class="pinned-order"
          :label="t('Pinned applications')"
          :item-label="(key) => key"
          :disabled="busy"
        >
          <template #default="{ item: key, index }">
            <div class="ordered-inline-row">
              <code>{{ key }}</code
              ><IconButton
                icon="close"
                class="secondary"
                :label="`${t('Remove')}: ${key}`"
                @click="draft.keys.splice(index, 1)"
              />
            </div>
          </template>
        </SortableList>
        <div class="form-actions">
          <input
            v-model="input"
            :aria-label="t('Application key')"
            placeholder="vendor/application"
            spellcheck="false"
            @keydown.enter.prevent="add"
          /><IconButton
            type="button"
            class="secondary"
            :disabled="!input.trim() || draft.keys.length >= 100"
            @click="add"
            icon="plus"
            :label="t('Add application')"
          />
        </div>
      </fieldset>
      <div class="form-actions">
        <button :disabled="busy">
          {{ saving ? t("Saving…") : t("Save changes") }}</button
        ><IconButton
          type="button"
          class="secondary"
          :disabled="busy"
          @click="load()"
          icon="refresh"
          :label="t('Reload')"
        />
      </div>
    </form>
    <p v-else-if="loading" role="status">{{ t("Loading…") }}</p>
  </section>
</template>
