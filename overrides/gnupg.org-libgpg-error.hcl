project = "gnupg.org/libgpg-error"
why     = "converted from gnupg.org-libgpg-error-darwin-gettext.patch; the reason is in the git history of those files"

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
