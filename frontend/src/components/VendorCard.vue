<script setup lang="ts">
import VendorLogo from "./VendorLogo.vue";
import EntityIcon from "./EntityIcon.vue";
import Icon from "./Icon.vue";
import IconButton from "./IconButton.vue";
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { directoryIcon, applicationPath, type ManagedApplication, type Vendor } from "../directory";
import { errorText, language, t } from "../i18n";
import { useVendorStrip } from "../composables/useVendorStrip";
export interface Card extends Vendor { apps: ManagedApplication[]; app_total: number; }
const props = defineProps<{ vendor: Card; query: string; state: string }>();
const list = reactive(useVendorStrip(computed(() => ({ id: props.vendor.id, query: props.query, state: props.state, apps: props.vendor.apps, total: props.vendor.app_total }))));
const filtered = computed(() => !!props.query || props.state !== "current");
const countLabel = computed(() => t(filtered.value
  ? list.total === 1 ? "{count} application in this view" : "{count} applications in this view"
  : list.total === 1 ? "{count} application" : "{count} applications", { count: list.total }));
const allPath = computed(() => ({ path: `/admin/vendors/${props.vendor.id}/apps`, query: {
  ...(props.query ? { q: props.query } : {}), ...(props.state !== "current" ? { state: props.state } : {}),
} }));
function stateLabel(app: ManagedApplication) {
  return app.deleted_at ? t("Deleted") : !app.enabled ? t("Disabled") : !props.vendor.enabled ? t("Disabled by vendor") : "";
}
const viewport = ref<HTMLElement>(), overflow = ref(false), atStart = ref(true), atEnd = ref(true);
const scrolling = ref(false), hovered = ref(false);
let scrollTimer: ReturnType<typeof setTimeout> | undefined;
function onScroll() {
  measure(); scrolling.value = true; clearTimeout(scrollTimer);
  scrollTimer = setTimeout(() => { scrolling.value = false; }, 700);
}
function keyboardScroll(event: KeyboardEvent) {
  if (event.target !== viewport.value) return;
  if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') { event.preventDefault(); scroll(event.key === 'ArrowLeft' ? -1 : 1); }
  if (event.key === 'Home' || event.key === 'End') { event.preventDefault(); viewport.value?.scrollTo({ left: event.key === 'Home' ? 0 : viewport.value.scrollWidth - viewport.value.clientWidth }); }
}
let observer: ResizeObserver | undefined;
function measure() {
  const el = viewport.value;
  if (!el) return;
  overflow.value = el.scrollWidth > el.clientWidth + 1;
  atStart.value = el.scrollLeft <= 1;
  atEnd.value = el.scrollLeft + el.clientWidth >= el.scrollWidth - 1;
}
function scroll(direction: number) {
  const el = viewport.value;
  if (!el) return;
  el.scrollBy({ left: direction * Math.max(96, el.clientWidth * .8), behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' });
}
watch(() => [list.items, props.vendor.uid, props.query, props.state], async (_, previous) => {
  await nextTick();
  if (previous && (previous[1] !== props.vendor.uid || previous[2] !== props.query || previous[3] !== props.state) && viewport.value) viewport.value.scrollLeft = 0;
  measure();
});
onMounted(() => {
  if (typeof ResizeObserver !== 'undefined') { observer = new ResizeObserver(measure); if (viewport.value) observer.observe(viewport.value); }
  window.addEventListener('resize', measure); measure();
});
onUnmounted(() => { clearTimeout(scrollTimer); observer?.disconnect(); window.removeEventListener('resize', measure); });
</script>
<template>
  <article class="panel vendor-card" :class="{ 'is-disabled': !vendor.enabled }" @mouseenter="hovered = true" @mouseleave="hovered = false">
    <div class="directory-heading">
      <div class="vendor-card-heading">
        <h2><RouterLink :to="`/admin/vendors/${vendor.id}/settings`" :title="vendor.name[language]">{{ vendor.name[language] }}</RouterLink></h2>
        <RouterLink class="app-count" :to="allPath" :title="countLabel">{{ countLabel }}</RouterLink>
      </div>
      <VendorLogo :vendor="vendor" admin :height="60" />
    </div>
    <div class="vendor-strip" :class="{ 'strip-can-left': overflow && !atStart, 'strip-can-right': overflow && !atEnd }">
      <IconButton v-if="overflow" class="strip-arrow strip-previous secondary" :class="{ 'strip-arrow-visible': hovered }" tabindex="-1" icon="left" :label="t('Scroll applications left')" :disabled="atStart" @click="scroll(-1)" />
      <div ref="viewport" class="vendor-preview-scroll" :class="{ 'is-scrolling': scrolling }" tabindex="0" role="region" :aria-label="t('Applications for {vendor}', { vendor: vendor.name[language] })" @scroll.passive="onScroll" @keydown="keyboardScroll">
        <ul class="directory-apps vendor-previews" :style="{ '--strip-count': Math.max(1, list.items.length + (vendor.deleted_at ? 0 : 1)) }">
          <li v-for="app in list.items" :key="app.uid" :class="{ 'is-disabled': !app.enabled || !vendor.enabled }">
            <RouterLink :to="applicationPath(app, app.deleted_at ? 'settings' : undefined)" :title="app.name[language]" :aria-label="stateLabel(app) ? `${app.name[language]} — ${stateLabel(app)}` : app.name[language]">
              <EntityIcon :src="directoryIcon(app.icon)" size="preview" />
              <span class="application-preview-name">{{ app.name[language] }}</span>
            </RouterLink>
          </li>
          <li v-if="!vendor.deleted_at" class="add-application">
            <RouterLink :to="`/admin/vendors/${vendor.id}/apps/new`" :aria-label="t('Add application')" :title="t('Add application')"><Icon name="plus" :size="44" /><span class="application-preview-name">{{ t('Add App') }}</span></RouterLink>
          </li>
        </ul>
      </div>
      <IconButton v-if="overflow" class="strip-arrow strip-next secondary" :class="{ 'strip-arrow-visible': hovered }" tabindex="-1" icon="right" :label="t('Scroll applications right')" :disabled="atEnd" @click="scroll(1)" />
    </div>
    <div class="vendor-strip-status">
      <span v-if="list.loading" role="status">{{ t('Loading…') }}</span>
      <template v-else-if="list.error"><span class="error" role="alert" :title="errorText(list.error)">{{ errorText(list.error) }}</span><IconButton class="secondary" icon="refresh" :label="t('Retry')" @click="list.refresh" /></template>
    </div>
  </article>
</template>
