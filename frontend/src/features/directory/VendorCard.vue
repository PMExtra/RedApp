<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from "vue";
import { ChevronLeft, ChevronRight, Plus } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import { useLocalized, vendorLogo } from "@/shared/i18n";
import { Badge, Button, EntityIcon, Spinner, cn } from "@/shared/ui";
import { appRoute, vendorRoute } from "./links";
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

// Scrolling: floating arrow buttons on hover for pointer users, arrow keys on
// the focused strip. The scrollbar only shows while the strip scrolls, and the
// edges fade where more applications are hidden.
const viewport = ref<HTMLElement>();
const atStart = ref(true);
const atEnd = ref(true);
const scrolling = ref(false);
let observer: ResizeObserver | undefined;
let scrollTimer: ReturnType<typeof setTimeout> | undefined;

// Hover-only overlay buttons: invisible and click-through until the strip is hovered.
const arrowClass =
  "pointer-events-none absolute top-8 flex size-8 -translate-y-1/2 items-center justify-center rounded-full border border-border bg-surface-raised text-fg opacity-0 shadow-md transition-opacity group-hover/strip:pointer-events-auto group-hover/strip:opacity-100 hover:bg-surface-hover [&_svg]:size-4";

const fade = computed(() => {
  if (!atStart.value && !atEnd.value) return "fade-x";
  if (!atStart.value) return "fade-start";
  if (!atEnd.value) return "fade-end";
  return "";
});

function measure(): void {
  const element = viewport.value;
  if (!element) return;
  atStart.value = element.scrollLeft <= 1;
  atEnd.value = element.scrollLeft + element.clientWidth >= element.scrollWidth - 1;
}

function onScroll(): void {
  measure();
  scrolling.value = true;
  clearTimeout(scrollTimer);
  scrollTimer = setTimeout(() => {
    scrolling.value = false;
  }, 800);
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
onBeforeUnmount(() => {
  observer?.disconnect();
  clearTimeout(scrollTimer);
});
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
    <header class="flex h-18 items-start justify-between gap-4">
      <div class="flex min-w-0 flex-col gap-1">
        <h2 :id="headingId" class="truncate text-base font-semibold">
          <RouterLink :to="vendorRoute(vendor.id)" class="rounded-sm hover:underline focus-ring">
            {{ name }}
          </RouterLink>
        </h2>
        <div class="flex h-5 min-w-0 items-center gap-2 overflow-hidden text-xs text-muted">
          <span class="truncate font-mono">{{ vendor.id }}</span>
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
          class="w-fit truncate rounded-sm text-sm text-primary hover:underline focus-ring"
        >
          {{ countLabel }}
        </RouterLink>
      </div>
      <EntityIcon
        :src="vendorLogo(vendor, locale)"
        :disabled="!vendor.enabled || !!vendor.deleted_at"
        variant="logo"
        size="lg"
        decorative
      />
    </header>

    <div class="group/strip relative">
      <div
        ref="viewport"
        role="region"
        tabindex="0"
        :aria-label="t('directory.vendorCard.strip', { vendor: name })"
        :class="cn('strip-scroll rounded-md pb-1 focus-ring', fade, scrolling && 'is-scrolling')"
        :aria-busy="(incomplete && all.isFetching.value) || undefined"
        @scroll.passive="onScroll"
        @keydown="onKeydown"
      >
        <ul class="flex h-[6.5rem] gap-1">
          <li v-for="app in apps" :key="app.uid" class="w-22 shrink-0">
            <RouterLink
              :to="appRoute(app)"
              :aria-label="appLabel(app)"
              :class="
                cn(
                  'flex h-full flex-col items-center gap-1.5 rounded-lg p-2 text-center text-xs hover:bg-surface-hover focus-ring',
                  appState(app) !== '' && 'opacity-60',
                )
              "
            >
              <EntityIcon
                :src="app.icon"
                size="lg"
                :disabled="appState(app) !== '' || !!vendor.deleted_at"
              />
              <span class="line-clamp-2 h-8 w-full break-words">
                {{ localized(app.name) || app.id }}
              </span>
            </RouterLink>
          </li>
          <li v-if="incomplete && all.isFetching.value" class="w-22 shrink-0" role="status">
            <span
              class="flex h-full flex-col items-center justify-center gap-1.5 p-2 text-center text-xs text-muted"
            >
              <Spinner />
              <span class="line-clamp-2 h-8">{{ t("directory.vendorCard.loadingApps") }}</span>
            </span>
          </li>
          <li v-else-if="incomplete && all.error.value" class="w-28 shrink-0" role="alert">
            <span
              class="flex h-full flex-col items-center justify-center gap-1.5 p-2 text-center text-xs text-danger"
            >
              <span class="line-clamp-3">{{ t("directory.vendorCard.appsFailed") }}</span>
              <Button size="sm" @click="all.refetch()">{{ t("common.actions.retry") }}</Button>
            </span>
          </li>
          <li v-if="!vendor.deleted_at" class="w-22 shrink-0">
            <RouterLink
              :to="{ name: 'admin-app-new', params: { vendor: vendor.id } }"
              class="flex h-full flex-col items-center justify-center gap-1.5 rounded-lg border border-dashed border-border-strong p-2 text-center text-xs text-muted hover:bg-surface-hover hover:text-fg focus-ring"
            >
              <Plus class="size-6" aria-hidden="true" />
              <span class="line-clamp-2">{{ t("directory.actions.addApp") }}</span>
            </RouterLink>
          </li>
        </ul>
      </div>
      <button
        v-if="!atStart"
        type="button"
        tabindex="-1"
        :aria-label="t('directory.vendorCard.scrollLeft')"
        :class="cn(arrowClass, 'start-1')"
        @click="scroll(-1)"
      >
        <ChevronLeft aria-hidden="true" />
      </button>
      <button
        v-if="!atEnd"
        type="button"
        tabindex="-1"
        :aria-label="t('directory.vendorCard.scrollRight')"
        :class="cn(arrowClass, 'end-1')"
        @click="scroll(1)"
      >
        <ChevronRight aria-hidden="true" />
      </button>
    </div>
  </article>
</template>
