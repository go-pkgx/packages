project = "gnu.org/coreutils"
why     = "converted from gnu.org-coreutils-darwin-gettext.patch; the reason is in the git history of those files"

edits = [
  {
    path = "dependencies"
    set = {
      "darwin" = {
        "gnu.org/gettext" = "*"
      }
    }
  },
]
