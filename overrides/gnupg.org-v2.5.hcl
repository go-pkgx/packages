project = "gnupg.org/v2.5"
why     = "converted from gnupg.org-v2.5-gettext-v1.patch; the reason is in the git history of those files"

edits = [
  {
    path = "dependencies.darwin[\"gnu.org/gettext\"]"
    set = "^1"
  },
]
