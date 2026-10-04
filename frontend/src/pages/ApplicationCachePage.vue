<script setup lang="ts">
import { applicationRecord, providerHasVersions } from "../directory";
import GeneralCachePage from "./GeneralCachePage.vue";
import Maintenance from "../components/Maintenance.vue";
import AutoCleanupStatus from "../components/AutoCleanupStatus.vue";
</script>
<template>
  <div v-if="applicationRecord" class="page-stack">
    <Maintenance
      v-if="providerHasVersions(applicationRecord.provider)"
      :application="applicationRecord.key"
    /><template v-else
      ><GeneralCachePage /><AutoCleanupStatus
        :application="applicationRecord.key"
    /></template>
  </div>
</template>
