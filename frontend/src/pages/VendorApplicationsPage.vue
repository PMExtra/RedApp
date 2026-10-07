<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";
import DirectoryApps from "../components/DirectoryApps.vue";
import { type Vendor } from "../directory";
defineProps<{ vendor: Vendor }>();
const route = useRoute();
const query = computed(() => typeof route.query.q === "string" ? route.query.q : "");
const state = computed(() => ["enabled", "disabled"].includes(String(route.query.state)) ? String(route.query.state) : "current");
</script>
<template>
  <section class="panel vendor-applications">
    <DirectoryApps :vendor="vendor" :query="query" :state="state" />
  </section>
</template>
