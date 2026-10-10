<script setup lang="ts">
import { TabsContent, TabsList, TabsRoot, TabsTrigger } from "reka-ui";
import type { TabItem } from "./types";

/**
 * In-page tabs; each panel is the slot named after the tab value. Hidden
 * panels are unmounted unless `keepMounted` is set, which panels holding
 * work in progress (a preview, a running job) need so switching tabs keeps it.
 */
const model = defineModel<string | undefined>();
defineProps<{ items: TabItem[]; label?: string; keepMounted?: boolean }>();
</script>

<template>
  <TabsRoot
    v-model="model"
    :default-value="items[0]?.value"
    :unmount-on-hide="!keepMounted"
    class="flex flex-col gap-4"
  >
    <TabsList :aria-label="label" class="flex gap-1 border-b border-border">
      <TabsTrigger
        v-for="item in items"
        :key="item.value"
        :value="item.value"
        :disabled="item.disabled"
        class="-mb-px border-b-2 border-transparent px-3 py-2 text-sm font-medium text-muted hover:text-fg focus-ring disabled:opacity-50 data-[state=active]:border-primary data-[state=active]:text-fg"
      >
        {{ item.label }}
      </TabsTrigger>
    </TabsList>
    <TabsContent v-for="item in items" :key="item.value" :value="item.value" class="focus-ring">
      <slot :name="item.value" />
    </TabsContent>
  </TabsRoot>
</template>
