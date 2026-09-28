project = "freedesktop.org/fontconfig"
why     = "fontconfig links gettext's libintl on darwin and did not declare it. Found by asking the mechanism rather than one report at a time: every darwin Mach-O on disk referencing libintl, cross-checked against what its project declares."

edits = [
  {
    path = "dependencies.darwin"
    set = {
      "gnu.org/gettext" = "^1"
    }
  },
]
