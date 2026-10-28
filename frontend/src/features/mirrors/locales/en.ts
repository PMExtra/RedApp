import type { LocaleModule } from "@/shared/i18n";

export default {
  mirrors: {
    list: {
      description:
        "Other RedApp instances whose published applications this instance can mirror. A mirror downloads through the upstream and follows its name, description, icon and usage instructions.",
      caption: "Upstreams",
      name: "Name",
      origin: "Address",
      mirrors: "Mirrored applications",
      created: "Added",
      actions: "Actions",
      empty: "No upstreams yet. Add another RedApp instance to mirror its applications.",
      add: "Add upstream",
      edit: "Edit {name}",
      delete: "Delete {name}",
      deleteTitle: "Delete upstream {name}?",
      deleteWarning:
        "Only the upstream entry is removed; no local application or file is affected.",
      deleted: "Upstream {name} deleted.",
      inUse:
        "{name} still has mirrored applications. Stop mirroring them (or delete them) before deleting the upstream.",
      conflict: "The upstream changed meanwhile. Its latest state is shown; try again.",
    },
    form: {
      addTitle: "Add upstream",
      editTitle: "Edit upstream {name}",
      description:
        "RedApp checks the address by reading the instance identity of the upstream before saving.",
      name: "Name",
      nameRequired: "Enter a name.",
      nameTooLong: "The name is too long.",
      origin: "Address",
      originHint:
        "http(s)://host[:port] of the upstream RedApp, without a path. Changing it makes every mirror of this upstream download from the new address.",
      originInvalid: "Enter an address like https://redapp.example.com or http://10.0.0.5:8080.",
      exists: "This address, or the instance behind it, is already an upstream.",
      added: "Upstream {name} added.",
      saved: "Upstream saved.",
    },
    upstream: {
      notFound: "Upstream not found",
      notFoundHint: "It may have been deleted.",
      back: "Back to upstreams",
      origin: "Address",
      instance: "Instance ID",
      mirrors: "Mirrored applications",
      edit: "Edit",
    },
    catalog: {
      title: "Published applications",
      description:
        "Applications the upstream publishes. Select the ones to mirror; hosted applications are mirrored as HTTP cache applications.",
      search: "Search applications",
      caption: "Applications of the upstream",
      select: "Select",
      selectApp: "Select {name}",
      name: "Name",
      key: "Key",
      provider: "Provider",
      status: "Status",
      mirroredAs: "Mirrored as {key}",
      unsupported: "Not supported",
      unsupportedHint: "This instance cannot mirror applications of this provider.",
      available: "Not mirrored",
      empty: "The upstream publishes no applications.",
      noMatches: "No application matches the search.",
      selected: "{count} selected",
      clear: "Clear selection",
      mirrorSelected: "Mirror selected",
    },
    mirror: {
      title: "Mirror applications",
      description:
        "Each application becomes a local application. The local vendor is created when it does not exist yet.",
      caption: "Applications to mirror",
      vendor: "Local vendor ID",
      app: "Local application ID",
      target: "Local application for {key}",
      checking: "Checking the local application…",
      isNew: "A new application is created.",
      exists: "{key} already exists.",
      adopt: "Adopt the existing application",
      adoptHint:
        "It keeps its local settings and files; its source becomes the upstream and its name, description, icon and usage instructions follow the upstream.",
      adoptRequired: "Confirm adopting the existing application or choose another ID.",
      providerMismatch:
        "The existing application uses {local}; an upstream {remote} application can only be mirrored into a {expected} application.",
      deletedTarget: "The existing application is deleted; choose another ID.",
      vendorInvalid:
        "Use lowercase letters, digits and hyphens; some names (admin, api, …) are reserved.",
      appInvalid: "Use lowercase letters, digits and hyphens.",
      submit: "Mirror",
      retry: "Mirror the remaining applications",
      done: "Mirrored as {key}",
      failed: "Failed",
      alreadyMirrored:
        "This upstream application is already mirrored, or the local ID is already taken. The local application was checked again.",
      recheck: "Check again",
      summary: "{done} of {total} applications mirrored.",
    },
    section: {
      title: "Mirror source",
      description:
        "This application follows an application of an upstream RedApp. Checks run automatically every 15 minutes.",
      upstream: "Upstream",
      remoteKey: "Upstream application",
      availability: "Availability",
      available: "Published",
      gone: "Upstream no longer publishes it",
      goneHint:
        "The upstream no longer publishes this application. It keeps serving its last values and cached files.",
      synced: "Last successful sync",
      checked: "Last check",
      lastError: "Last error",
      noError: "None",
      check: "Check now",
      checkedToast: "Checked the upstream.",
      unbind: "Stop mirroring",
      unbindTitle: "Stop mirroring {key}?",
      unbindWarning:
        "The application keeps its current name, description, icon, instructions, source and files, and becomes an ordinary application that you edit yourself. It no longer follows the upstream.",
      unbound: "{key} no longer follows its upstream.",
      conflict: "The application changed meanwhile. Its latest state is shown; try again.",
    },
  },
} satisfies LocaleModule;
