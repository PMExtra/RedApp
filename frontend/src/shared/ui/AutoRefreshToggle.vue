<script setup lang="ts">
import { useId } from "vue";
import { useI18n } from "vue-i18n";
import Switch from "./Switch.vue";

/** Pair with `useAutoRefresh()`: `<AutoRefreshToggle v-model="auto.enabled.value" />`. */
const enabled = defineModel<boolean>({ default: true });
const { t } = useI18n();
const id = useId();
</script>

<template>
  <span class="inline-flex items-center gap-2 text-sm">
    <Switch :id="id" v-model="enabled" />
    <label :for="id">{{ t("ui.autoRefresh") }}</label>
  </span>
</template>
