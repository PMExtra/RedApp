<script setup lang="ts">
import { computed } from "vue";
import { Search, Plus, GripVertical, Trash2, LayoutGrid, Box, Activity, Settings2, RefreshCw, User, ChevronLeft, ChevronRight, ChevronDown, ArrowRight, ArrowLeft, Copy, Check, X, Lock, LogOut, Globe, Clock, TriangleAlert, ArrowUpDown, ArrowUp, ArrowDown, Info, Pencil, Tags, Download, Upload, CopyPlus } from "@lucide/vue";
const props = withDefaults(defineProps<{ name: string; size?: number }>(), { size: 18 });
const icons = { search: Search, plus: Plus, reorder: GripVertical, trash: Trash2, grid: LayoutGrid, box: Box, activity: Activity, settings: Settings2, refresh: RefreshCw, user: User, left: ChevronLeft, right: ChevronRight, chevron: ChevronDown, arrow: ArrowRight, back: ArrowLeft, copy: Copy, check: Check, close: X, lock: Lock, exit: LogOut, globe: Globe, clock: Clock, alert: TriangleAlert, sort: ArrowUpDown, ascending: ArrowUp, descending: ArrowDown, info: Info, edit: Pencil, tags: Tags, download: Download, upload: Upload, duplicate: CopyPlus };
const icon = computed(() => icons[props.name as keyof typeof icons] || LayoutGrid);
</script>
<template><component :is="icon" :size="size" :stroke-width="1.7" aria-hidden="true" focusable="false" /></template>
