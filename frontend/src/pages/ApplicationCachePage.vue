<script setup lang="ts">
import { applicationRecord, providerHasVersions } from "../directory";
import GeneralCachePage from "./GeneralCachePage.vue";
import RetentionSettings from "../components/RetentionSettings.vue";
import PrewarmSettings from "../components/PrewarmSettings.vue";
import Maintenance from "../components/Maintenance.vue";
import AutoCleanupStatus from "../components/AutoCleanupStatus.vue";
</script>
<template>
  <div v-if="applicationRecord" class="page-stack">
    <PrewarmSettings :key="`${applicationRecord.key}:${applicationRecord.provider}`" v-if="['codex','claude-code','http-cache'].includes(applicationRecord.provider)" :application="applicationRecord.key" />
    <RetentionSettings
      v-if="providerHasVersions(applicationRecord.provider)"
      :application="applicationRecord.key"
    />
    <Maintenance
      v-if="providerHasVersions(applicationRecord.provider)"
      :application="applicationRecord.key"
    /><template v-else
      ><GeneralCachePage /><AutoCleanupStatus
        :application="applicationRecord.key"
    /></template>
  </div>
</template>
