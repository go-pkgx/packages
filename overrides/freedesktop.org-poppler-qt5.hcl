project = "freedesktop.org/poppler-qt5"
why     = "gettext renumbered 0.26 to 1.0.0 and kept the same library — both bottles ship the same libintl. This recipe pins the 0 major, so it now excludes the current release rather than describing a compatibility boundary, and a closure holding both generations resolves whichever constraint it read last. An environment-module stack has to build ONE coherent generation."

edits = [
  {
    path = "dependencies[\"gnu.org/gettext\"]"
    set  = "^1"
  },
]
