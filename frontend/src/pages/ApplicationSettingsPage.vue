<script setup lang="ts">
import DirectoryEditor from "../components/DirectoryEditor.vue";
import GeneralCachePolicy from "../components/GeneralCachePolicy.vue";
import ChannelSettings from "../components/ChannelSettings.vue";
import ApplicationInstructions from "../components/ApplicationInstructions.vue";
import { applicationRecord as app, providerHasVersions } from "../directory";
</script>
<template>
  <div v-if="app" class="page-stack application-settings">
    <DirectoryEditor kind="app" /><ApplicationInstructions
      :application="app.key"
      :readonly="!!app.deleted_at"
    /><template v-if="!app.deleted_at"
      ><ChannelSettings
        v-if="providerHasVersions(app.provider)"
        :application="app.key" /><GeneralCachePolicy
        v-else-if="app.provider === 'http-cache'"
        :application="app.key"
    /></template>
  </div>
</template>
