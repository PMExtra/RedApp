<script setup lang="ts">
import { computed } from "vue";
import EntityIcon from "./EntityIcon.vue";
import { directoryIcon } from "../directory";
import { language } from "../i18n";
import type { LocalizedText } from "../site";
const props = defineProps<{ vendor?: { icon?: string; localized_icons?: Partial<LocalizedText> }; admin?: boolean }>();
const asset = (path = "") => props.admin ? directoryIcon(path) : path;
const preferred = computed(() => asset(props.vendor?.localized_icons?.[language.value] || props.vendor?.icon));
const fallback = computed(() => asset(props.vendor?.icon));
</script>
<template><EntityIcon :src="preferred" :fallback-src="fallback" vendor /></template>
