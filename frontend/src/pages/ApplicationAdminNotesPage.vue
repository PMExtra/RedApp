<script setup lang="ts">
import AdminNotesEditor from "../components/AdminNotesEditor.vue";
import { applicationRecord as app } from "../directory";
</script>
<template><AdminNotesEditor v-if="app" :endpoint="`apps/${app.key}/admin-notes`" :readonly="!!app.deleted_at" /></template>
