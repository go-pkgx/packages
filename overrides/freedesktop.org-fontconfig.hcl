project = "freedesktop.org/fontconfig"
why     = "converted from freedesktop.org-fontconfig-darwin-gettext.patch; the reason is in the git history of those files"

edits = [
  {
    path = "dependencies.darwin"
    set = {
      "gnu.org/gettext" = "^1"
    }
  },
]
