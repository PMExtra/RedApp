<script setup lang="ts">
import DirectoryApps from "../components/DirectoryApps.vue";
import { applicationNavigation, type Vendor } from "../directory";
import { language } from "../i18n";
defineProps<{ vendor: Vendor }>();
</script>
<template>
  <section class="panel vendor-applications">
    <h2>{{ applicationNavigation[language] }}</h2>
    <DirectoryApps :vendor="vendor" query="" state="current" />
  </section>
</template>
