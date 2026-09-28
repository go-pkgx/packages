project = "gnu.org/gawk"
why     = "gawk links gettext's libintl on darwin and did not declare it, so the darwin bottle does not start from an empty store."

edits = [
  {
    path = "dependencies"
    set = {
      "darwin" = {
        "gnu.org/gettext" = "^1"
      }
    }
  },
]
