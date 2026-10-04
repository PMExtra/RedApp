<script setup lang="ts">
import HomepageSettings from "../components/HomepageSettings.vue";
import SiteSettings from "../components/SiteSettings.vue";
import PublicURLSettings from "../components/PublicURLSettings.vue";
</script>
<template>
  <div class="settings-stack">
    <SiteSettings /><PublicURLSettings /><HomepageSettings />
  </div>
</template>
