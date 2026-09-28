project = "gnu.org/help2man"
why     = "help2man pins gettext ^0, which is stale: gettext renumbered 0.26 to 1.0.0 and kept the same library. The dead pin blocks gcc two levels up."

edits = [
  {
    path = "dependencies[\"gnu.org/gettext\"]"
    set  = "*"
  },
]
