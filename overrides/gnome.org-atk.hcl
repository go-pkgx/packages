project = "gnome.org/atk"
why     = "converted from gnome.org-atk-gettext-v1.patch; the reason is in the git history of those files"

edits = [
  {
    path = "build.dependencies[\"gnu.org/gettext\"]"
    set = "^1"
  },
]
