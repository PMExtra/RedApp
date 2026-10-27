<script setup lang="ts">
import { Ellipsis } from "@lucide/vue";
import {
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuPortal,
  DropdownMenuRoot,
  DropdownMenuTrigger,
} from "reka-ui";
import { useI18n } from "vue-i18n";
import type { Schema } from "@/shared/api";
import { Button, itemClass, panelClass } from "@/shared/ui";

defineProps<{ items: Schema<"NavigationItem">[] }>();
const { t, locale } = useI18n();
// Fixed limit, not DOM measurement: the remaining items always live in More.
const visibleLimit = 2;
function title(item: Schema<"NavigationItem">) {
  const language = locale.value === "zh-CN" ? "zh-CN" : "en";
  return (
    item.localized_title?.[language] ||
    item.localized_title?.[language === "en" ? "zh-CN" : "en"] ||
    item.title
  );
}
</script>

<template>
  <nav :aria-label="t('publicShell.nav.custom')" class="flex min-w-0 items-center gap-1">
    <Button
      v-for="(item, index) in items.slice(0, visibleLimit)"
      :key="index"
      as-child
      variant="ghost"
      size="sm"
      class="hidden max-w-24 xl:inline-flex"
    >
      <a
        :href="item.url"
        :title="title(item)"
        :target="item.new_tab ? '_blank' : undefined"
        :rel="item.new_tab ? 'noopener noreferrer' : undefined"
      >
        <span class="truncate">{{ title(item) }}</span>
        <span v-if="item.new_tab" class="sr-only"> {{ t("publicShell.nav.newTab") }}</span>
      </a>
    </Button>
    <DropdownMenuRoot :modal="false">
      <DropdownMenuTrigger as-child>
        <Button
          variant="ghost"
          size="sm"
          :aria-label="t('publicShell.nav.more')"
          :class="items.length <= visibleLimit ? 'xl:hidden' : ''"
        >
          <Ellipsis aria-hidden="true" />
          <span class="hidden sm:inline">{{ t("publicShell.nav.more") }}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuPortal>
        <DropdownMenuContent
          align="start"
          :side-offset="6"
          :collision-padding="8"
          :class="[
            panelClass,
            'max-h-(--reka-dropdown-menu-content-available-height) w-64 max-w-[calc(100vw-1rem)] overflow-y-auto',
          ]"
        >
          <DropdownMenuItem
            v-for="(item, index) in items"
            :key="index"
            as-child
            :class="[itemClass, index < visibleLimit && 'xl:hidden']"
          >
            <a
              :href="item.url"
              :target="item.new_tab ? '_blank' : undefined"
              :rel="item.new_tab ? 'noopener noreferrer' : undefined"
            >
              <span class="min-w-0 whitespace-normal break-words">{{ title(item) }}</span>
              <span v-if="item.new_tab" class="sr-only"> {{ t("publicShell.nav.newTab") }}</span>
            </a>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenuPortal>
    </DropdownMenuRoot>
  </nav>
</template>
