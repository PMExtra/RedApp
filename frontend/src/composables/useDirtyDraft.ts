import { getCurrentInstance, onMounted, onUnmounted, type Ref } from "vue";
import { onBeforeRouteLeave, onBeforeRouteUpdate, routerKey } from "vue-router";
import { t } from "../i18n";
import { signedIn } from "../session";
const activeDrafts = new Set<Ref<boolean>>();
export function confirmDirtyDrafts() {
  return (
    ![...activeDrafts].some((draft) => draft.value) ||
    window.confirm(t("Discard unsaved changes?"))
  );
}
export function useDirtyDraft(dirty: Ref<boolean>) {
  activeDrafts.add(dirty);
  const confirmDiscard = () =>
    !dirty.value || window.confirm(t("Discard unsaved changes?"));
  if (getCurrentInstance()?.appContext.provides[routerKey as symbol]) {
    const guard = () => !signedIn.value || confirmDiscard();
    onBeforeRouteLeave(guard);
    onBeforeRouteUpdate(guard);
  }
  const beforeUnload = (event: BeforeUnloadEvent) => {
    if (dirty.value) {
      event.preventDefault();
      event.returnValue = "";
    }
  };
  onMounted(() => window.addEventListener("beforeunload", beforeUnload));
  onUnmounted(() => {
    activeDrafts.delete(dirty);
    window.removeEventListener("beforeunload", beforeUnload);
  });
  return confirmDiscard;
}
