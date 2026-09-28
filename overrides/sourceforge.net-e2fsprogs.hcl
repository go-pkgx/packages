project = "sourceforge.net/e2fsprogs"
why     = "converted from sourceforge.net-e2fsprogs-gettext-v1.patch; the reason is in the git history of those files"

edits = [
  {
    path = "dependencies.darwin[\"gnu.org/gettext\"]"
    set = "^1"
  },
]
