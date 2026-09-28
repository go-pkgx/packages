project = "darwinsys.com/file"
why     = "converted from darwinsys.com-file-compressors.patch; the reason is in the git history of those files"

edits = [
  {
    path = "dependencies[\"sourceware.org/bzip2\"]"
    set = "^1"
  },
  {
    path = "dependencies[\"tukaani.org/xz\"]"
    set = "^5"
  },
]
