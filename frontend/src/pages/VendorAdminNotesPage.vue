<script setup lang="ts">
import AdminNotesEditor from "../components/AdminNotesEditor.vue";
import type { Vendor } from "../directory";
defineProps<{ vendor: Vendor }>();
</script>
<template><AdminNotesEditor :endpoint="`vendors/${vendor.id}/admin-notes`" :readonly="!!vendor.deleted_at" /></template>
