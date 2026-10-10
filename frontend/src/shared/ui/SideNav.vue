<script setup lang="ts">
import { RouterLink } from "vue-router";
import type { SideNavSection } from "./types";

defineProps<{ sections: SideNavSection[]; label: string }>();
const link =
  "flex items-center gap-2.5 rounded-md px-2.5 py-1.5 text-sm text-muted hover:bg-surface-hover hover:text-fg focus-ring [&_svg]:size-4";
</script>

<template>
  <nav :aria-label="label" class="flex flex-col gap-5">
    <div
      v-for="(section, index) in sections"
      :key="section.label ?? index"
      class="flex flex-col gap-1"
    >
      <p
        v-if="section.label"
        class="px-2.5 text-xs font-medium tracking-wide text-subtle uppercase"
      >
        {{ section.label }}
      </p>
      <ul class="flex flex-col gap-0.5">
        <li v-for="item in section.items" :key="item.label">
          <RouterLink v-slot="{ href, navigate, isActive, isExactActive }" :to="item.to" custom>
            <a
              :href="href"
              :class="[
                link,
                (item.exact ? isExactActive : isActive) && 'bg-primary-soft !text-primary',
              ]"
              :aria-current="(item.exact ? isExactActive : isActive) ? 'page' : undefined"
              @click="navigate"
            >
              <component :is="item.icon" v-if="item.icon" aria-hidden="true" />
              {{ item.label }}
            </a>
          </RouterLink>
        </li>
      </ul>
    </div>
  </nav>
</template>
