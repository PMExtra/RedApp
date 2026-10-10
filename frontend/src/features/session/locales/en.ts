import type { LocaleModule } from "@/shared/i18n";

export default {
  session: {
    login: {
      title: "Sign in",
      description: "Enter the administrator password.",
      password: "Password",
      passwordRequired: "Enter the password.",
      submit: "Sign in",
      signingIn: "Signing in…",
    },
    notices: {
      signedOut: "You have signed out.",
      passwordChanged: "Password changed. Sign in with the new password.",
      expired: "Your session expired. Sign in again.",
    },
    expired: {
      title: "Session expired",
      description: "Sign in again to continue. Your unsaved changes on this page are kept.",
    },
    checkFailed: "The session could not be checked.",
    account: {
      menu: "Administrator",
      changePassword: "Change password",
      publicSite: "Public site",
      signOut: "Sign out",
    },
    password: {
      title: "Change password",
      description: "All sessions, including this one, are signed out after the change.",
      current: "Current password",
      new: "New password",
      confirm: "Confirm new password",
      lengthRule: "12 to 72 bytes.",
      mismatch: "The passwords do not match.",
      submit: "Change password",
    },
  },
} satisfies LocaleModule;
