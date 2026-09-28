project = "open-mpi.org/hwloc"
why     = "hwloc declares NO runtime dependencies and links libxml2, so a bottle installed into an empty store cannot start."

edits = [
  {
    path = "dependencies"
    set = {
      "gnome.org/libxml2" = ">=2.14"
    }
  },
]
