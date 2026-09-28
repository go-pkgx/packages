project = "freedesktop.org/xdg-user-dirs"
why     = "converted from freedesktop.org-xdg-user-dirs-gettext-v1.patch; the reason is in the git history of those files"

edits = [
  {
    path = "dependencies[\"gnu.org/gettext\"]"
    set = "^1"
  },
]
