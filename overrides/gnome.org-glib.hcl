project = "gnome.org/glib"
why     = "converted from gnome.org-glib-gettext-v1.patch; the reason is in the git history of those files"

edits = [
  {
    path = "dependencies[\"gnu.org/gettext\"]"
    set = "^1"
  },
]
