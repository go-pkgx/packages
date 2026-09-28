project = "gnu.org/gawk"
why     = "converted from gnu.org-gawk-darwin-gettext.patch; the reason is in the git history of those files"

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
