<script setup lang="ts">
import { computed, type Component } from "vue";
import { useRoute } from "vue-router";
import {
  AppGeneralForm,
  AppInstructionsForm,
  DeleteSection,
  useApp,
  type ProviderKey,
} from "@/features/directory";
import { AppTaxonomyForm } from "@/features/taxonomy";

/**
 * Settings tab: details, upstream and proxy; taxonomy; usage instructions;
 * provider-specific sections; deletion. Every section edits the same
 * configuration overlay through `@/features/configuration`, so saving one
 * section never conflicts with another.
 */
interface ProviderSection {
  /** Stable key (used as the v-for key). */
  id: string;
  /** Providers that show the section. */
  providers: readonly ProviderKey[];
  /**
   * Receives `{ vendor: string; app: string; readOnly: boolean }`. It reads and
   * saves through `useAppConfiguration` / `useAppConfigurationPatch` and keeps
   * its own draft, conflict alert and leave guard (see `useOverlayForm`).
   */
  component: Component;
}

/*
 * EXTENSION POINT (package C, cache): add the runtime settings sections here,
 * e.g.
 *   { id: "cache-policy", providers: ["http-cache"], component: HttpPolicySection },
 *   { id: "channel-ttl", providers: ["codex", "claude-code"], component: ChannelTtlSection },
 * They render after the instructions, in this order, and are hidden for
 * deleted applications. `cache_ttl_seconds` of `http-cache` is already edited
 * in the upstream section of AppGeneralForm; the channel TTL section is for
 * the release providers only.
 */
const providerSections: readonly ProviderSection[] = [];

const route = useRoute();
const vendorId = computed(() => String(route.params.vendor));
const appId = computed(() => String(route.params.app));
const app = useApp(vendorId, appId);
const readOnly = computed(() => app.data.value?.deleted_at != null);
const sections = computed(() => {
  const data = app.data.value;
  if (!data || data.deleted_at) return [];
  return providerSections.filter((section) => section.providers.includes(data.provider));
});
</script>

<template>
  <div v-if="app.data.value" :key="app.data.value.uid" class="flex flex-col gap-6">
    <AppGeneralForm :app="app.data.value" />
    <AppTaxonomyForm :vendor="vendorId" :app="appId" :read-only="readOnly" />
    <AppInstructionsForm :app="app.data.value" />
    <component
      :is="section.component"
      v-for="section in sections"
      :key="section.id"
      :vendor="vendorId"
      :app="appId"
      :read-only="readOnly"
    />
    <DeleteSection v-if="!readOnly" :entity="app.data.value" />
  </div>
</template>
