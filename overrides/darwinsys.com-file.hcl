project = "darwinsys.com/file"
why     = "`file` links three compressors and the recipe declared one. From an empty store it dies on whichever is missing, one at a time, so fixing them singly costs three rebuilds to learn one thing."

edits = [
  {
    path = "dependencies[\"sourceware.org/bzip2\"]"
    set  = "^1"
  },
  {
    path = "dependencies[\"tukaani.org/xz\"]"
    set  = "^5"
  },
]
