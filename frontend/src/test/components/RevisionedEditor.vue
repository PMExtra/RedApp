<script setup lang="ts">
// Test fixture: a minimal settings editor following the documented
// load → draft → conditional save → conflict pattern.
import { ref, watch } from "vue";
import { useQuery } from "@tanstack/vue-query";
import { api, queryKey, unwrap, useRevisionedMutation } from "@/shared/api";
import { RevisionConflictAlert } from "@/shared/ui";

const key = queryKey("getSiteSettings");
const query = useQuery({
  queryKey: key,
  queryFn: () => unwrap(api.GET("/admin/api/settings/site")),
});
const draft = ref("");
watch(
  () => query.data.value,
  (data) => {
    if (data) draft.value = data.title.en;
  },
  { immediate: true },
);
const save = useRevisionedMutation({
  revision: () => query.data.value?.revision,
  queryKey: key,
  mutationFn: (title: string, ifMatch) => {
    const current = query.data.value;
    if (!current) throw new Error("not loaded");
    return unwrap(
      api.PUT("/admin/api/settings/site", {
        params: { header: { "If-Match": ifMatch } },
        body: {
          title: { ...current.title, en: title },
          subtitle: current.subtitle,
          disclaimer: current.disclaimer,
        },
      }),
    );
  },
});
</script>

<template>
  <div>
    <label>Title <input v-model="draft" /></label>
    <p>Saved: {{ query.data.value?.title.en }} (r{{ query.data.value?.revision }})</p>
    <RevisionConflictAlert v-if="save.hasConflict.value" @reload="save.reload()" />
    <button type="button" @click="save.mutate(draft)">Save</button>
  </div>
</template>
