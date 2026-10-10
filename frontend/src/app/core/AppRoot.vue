<script setup lang="ts">
import { RouterView } from "vue-router";
import { ConfirmHost, Toaster, TooltipProvider } from "@/shared/ui";
</script>

<template>
  <TooltipProvider :delay-duration="400">
    <RouterView />
    <Toaster />
    <ConfirmHost />
  </TooltipProvider>
</template>
