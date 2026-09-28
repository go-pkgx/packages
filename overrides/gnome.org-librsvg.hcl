project = "gnome.org/librsvg"
why     = "converted from gnome.org-librsvg-gettext-v1.patch; the reason is in the git history of those files"

edits = [
  {
    path = "dependencies[\"gnu.org/gettext\"]"
    set = "^1"
  },
]
