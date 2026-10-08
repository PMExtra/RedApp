<script setup lang="ts">
import ApplicationTaxonomy from "../components/ApplicationTaxonomy.vue";
import TemplateReset from "../components/TemplateReset.vue";
import { ref } from "vue";
import { refreshApplication } from "../directory";
const reload = ref(0);
async function resetSaved() {
  reload.value++;
  await refreshApplication();
}
import DirectoryEditor from "../components/DirectoryEditor.vue";
import GeneralCachePolicy from "../components/GeneralCachePolicy.vue";
import ChannelSettings from "../components/ChannelSettings.vue";
import ApplicationInstructions from "../components/ApplicationInstructions.vue";
import { applicationRecord as app, providerHasVersions } from "../directory";
</script>
<template>
  <div v-if="app" class="page-stack application-settings">
    <DirectoryEditor :key="reload" kind="app" /><ApplicationTaxonomy :key="reload" :application="app.key" :readonly="!!app.deleted_at" /><TemplateReset
      v-if="app.builtin_template && !app.deleted_at"
      :application="app.key"
      @saved="resetSaved"
    /><ApplicationInstructions
      :key="reload"
      :application="app.key"
      :readonly="!!app.deleted_at"
    /><template v-if="!app.deleted_at"
      ><ChannelSettings
        :key="reload"
        v-if="providerHasVersions(app.provider)"
        :application="app.key" /><GeneralCachePolicy
        :key="reload"
        v-else-if="app.provider === 'http-cache'"
        :application="app.key"
    /></template>
  </div>
</template>
