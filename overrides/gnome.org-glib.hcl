project = "gnome.org/glib"
why     = "glib pinned gettext's ^0.21 while python.org, fontconfig and four others pin ^1. Both cannot hold, and the resolver kept whichever constraint it read last — one closure has to hold one gettext."

edits = [
  {
    path = "dependencies[\"gnu.org/gettext\"]"
    set  = "^1"
  },
]
