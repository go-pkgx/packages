project = "gnupg.org"
why     = "gnupg pins gettext's 0 major on darwin, which is stale after the 0.26-to-1.0.0 renumbering, and it links libraries its recipe does not declare."

edits = [
  {
    path = "dependencies.darwin[\"gnu.org/gettext\"]"
    set  = "^1"
  },
]
