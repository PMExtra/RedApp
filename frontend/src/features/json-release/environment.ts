import { onMounted, ref, type Ref } from "vue";
import { detectEnvironment, type Environment } from "./recommendation";

interface HighEntropyHints {
  platform?: string;
  architecture?: string;
  bitness?: string;
}

/**
 * The visitor's OS and architecture: the user agent first, then client hints
 * where the browser offers them. Detection is a suggestion only.
 */
export function useEnvironment(): Ref<Environment> {
  const environment = ref<Environment>({});
  onMounted(async () => {
    environment.value = detectEnvironment(navigator.userAgent);
    const browser = navigator as Navigator & {
      userAgentData?: { getHighEntropyValues: (keys: string[]) => Promise<HighEntropyHints> };
    };
    try {
      const hints = await browser.userAgentData?.getHighEntropyValues([
        "platform",
        "architecture",
        "bitness",
      ]);
      if (hints) environment.value = detectEnvironment(navigator.userAgent, hints);
    } catch {
      /* The user agent result stands. */
    }
  });
  return environment;
}
