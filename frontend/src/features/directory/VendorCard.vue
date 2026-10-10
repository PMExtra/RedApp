<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from "vue";
import { ChevronLeft, ChevronRight, Plus } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import { useLocalized } from "@/shared/i18n";
import { Badge, Button, EntityIcon, Spinner, cn } from "@/shared/ui";
import { appRoute, vendorRoute } from "./links";
import { vendorLogo } from "./logo";
import { useAllVendorApps, type App, type DirectoryState, type VendorListItem } from "./queries";

/**
 * One vendor of the directory grid: name, logo and a horizontally scrolling
 * strip of its matching applications. The list response previews five
 * applications; the remaining pages are loaded for the strip.
 */
const props = defineProps<{ vendor: VendorListItem; q: string; state: DirectoryState }>();
const { t, locale } = useI18n();
const localized = useLocalized();
const headingId = useId();

const incomplete = computed(() => props.vendor.app_total > props.vendor.apps.length);
const all = useAllVendorApps(
  () => ({ vendor: props.vendor.id, q: props.q, state: props.state }),
  incomplete,
);
const apps = computed<App[]>(() =>
  incomplete.value && all.data.value ? all.data.value.items : props.vendor.apps,
);
const total = computed(() =>
  incomplete.value && all.data.value ? all.data.value.total : props.vendor.app_total,
);
const filtered = computed(() => props.q !== "" || props.state !== "current");
const countLabel = computed(() =>
  t(
    filtered.value ? "directory.vendorCard.matching" : "directory.vendorCard.count",
    { count: total.value },
    total.value,
  ),
);
const appsLink = computed(() =>
  vendorRoute(props.vendor.id, "apps", {
    ...(props.q ? { q: props.q } : {}),
    ...(props.state !== "current" ? { state: props.state } : {}),
  }),
);
const name = computed(() => localized(props.vendor.name) || props.vendor.id);

function appState(app: App): string {
  if (app.deleted_at) return t("directory.state.deletedOne");
  if (!app.enabled) return t("directory.state.disabledOne");
  if (!props.vendor.enabled) return t("directory.state.disabledByVendor");
  return "";
}

function appLabel(app: App): string {
  const label = localized(app.name) || app.id;
  const state = appState(app);
  return state ? `${label} (${state})` : label;
}

// Scrolling: arrow buttons for pointer users, arrow keys on the focused strip.
const viewport = ref<HTMLElement>();
const atStart = ref(true);
const atEnd = ref(true);
let observer: ResizeObserver | undefined;

function measure(): void {
  const element = viewport.value;
  if (!element) return;
  atStart.value = element.scrollLeft <= 1;
  atEnd.value = element.scrollLeft + element.clientWidth >= element.scrollWidth - 1;
}

function scroll(direction: -1 | 1): void {
  const element = viewport.value;
  if (!element) return;
  const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  element.scrollBy({
    left: direction * Math.max(120, element.clientWidth * 0.8),
    behavior: reduce ? "auto" : "smooth",
  });
}

function onKeydown(event: KeyboardEvent): void {
  if (event.target !== viewport.value) return;
  if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
    event.preventDefault();
    scroll(event.key === "ArrowLeft" ? -1 : 1);
  }
}

watch(apps, () => void nextTick(measure));
watch(
  () => [props.vendor.id, props.q, props.state],
  () => {
    if (viewport.value) viewport.value.scrollLeft = 0;
  },
);
onMounted(() => {
  if (typeof ResizeObserver !== "undefined" && viewport.value) {
    observer = new ResizeObserver(measure);
    observer.observe(viewport.value);
  }
  measure();
});
onBeforeUnmount(() => observer?.disconnect());
</script>

<template>
  <article
    :aria-labelledby="headingId"
    :class="
      cn(
        'flex min-w-0 flex-col gap-4 rounded-xl border border-border bg-surface p-5 shadow-sm',
        (!vendor.enabled || vendor.deleted_at) && 'bg-surface-sunken',
      )
    "
  >
    <header class="flex items-start justify-between gap-4">
      <div class="flex min-w-0 flex-col gap-1">
        <h2 :id="headingId" class="truncate text-base font-semibold">
          <RouterLink :to="vendorRoute(vendor.id)" class="rounded-sm hover:underline focus-ring">
            {{ name }}
          </RouterLink>
        </h2>
        <div class="flex flex-wrap items-center gap-2 text-xs text-muted">
          <span class="font-mono">{{ vendor.id }}</span>
          <Badge v-if="vendor.deleted_at" tone="danger">{{
            t("directory.state.deletedOne")
          }}</Badge>
          <Badge v-else-if="!vendor.enabled" tone="warning">
            {{ t("directory.state.disabledOne") }}
          </Badge>
          <Badge v-if="vendor.has_template">{{ t("directory.builtin") }}</Badge>
        </div>
        <RouterLink
          :to="appsLink"
          class="w-fit rounded-sm text-sm text-primary hover:underline focus-ring"
        >
          {{ countLabel }}
        </RouterLink>
      </div>
      <EntityIcon :src="vendorLogo(vendor, locale)" variant="logo" size="lg" />
    </header>

    <div class="flex items-center gap-1">
      <Button
        v-if="!atStart || !atEnd"
        icon
        size="sm"
        variant="ghost"
        class="hidden sm:inline-flex"
        tabindex="-1"
        :aria-label="t('directory.vendorCard.scrollLeft')"
        :disabled="atStart"
        @click="scroll(-1)"
      >
        <ChevronLeft aria-hidden="true" />
      </Button>
      <div
        ref="viewport"
        role="region"
        tabindex="0"
        :aria-label="t('directory.vendorCard.strip', { vendor: name })"
        class="min-w-0 flex-1 overflow-x-auto rounded-md pb-1 focus-ring"
        @scroll.passive="measure"
        @keydown="onKeydown"
      >
        <ul class="flex gap-1">
          <li v-for="app in apps" :key="app.uid" class="w-24 shrink-0">
            <RouterLink
              :to="appRoute(app)"
              :aria-label="appLabel(app)"
              :class="
                cn(
                  'flex flex-col items-center gap-1.5 rounded-lg p-2 text-center text-xs hover:bg-surface-hover focus-ring',
                  appState(app) !== '' && 'opacity-60',
                )
              "
            >
              <EntityIcon :src="app.icon" size="lg" />
              <span class="line-clamp-2 w-full break-words">
                {{ localized(app.name) || app.id }}
              </span>
            </RouterLink>
          </li>
          <li v-if="!vendor.deleted_at" class="w-24 shrink-0">
            <RouterLink
              :to="{ name: 'admin-app-new', params: { vendor: vendor.id } }"
              class="flex h-full flex-col items-center justify-center gap-1.5 rounded-lg border border-dashed border-border-strong p-2 text-center text-xs text-muted hover:bg-surface-hover hover:text-fg focus-ring"
            >
              <Plus class="size-6" aria-hidden="true" />
              {{ t("directory.actions.addApp") }}
            </RouterLink>
          </li>
        </ul>
      </div>
      <Button
        v-if="!atStart || !atEnd"
        icon
        size="sm"
        variant="ghost"
        class="hidden sm:inline-flex"
        tabindex="-1"
        :aria-label="t('directory.vendorCard.scrollRight')"
        :disabled="atEnd"
        @click="scroll(1)"
      >
        <ChevronRight aria-hidden="true" />
      </Button>
    </div>

    <p
      v-if="incomplete && all.isFetching.value"
      class="flex items-center gap-2 text-xs text-muted"
      role="status"
    >
      <Spinner size="sm" /> {{ t("directory.vendorCard.loadingApps") }}
    </p>
    <div
      v-else-if="incomplete && all.error.value"
      class="flex items-center gap-2 text-xs text-danger"
      role="alert"
    >
      {{ t("directory.vendorCard.appsFailed") }}
      <Button size="sm" @click="all.refetch()">{{ t("common.actions.retry") }}</Button>
    </div>
  </article>
</template>
