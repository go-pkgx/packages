project = "rsync.samba.org"
why     = "converted from rsync.samba.org-declare-libidn2.patch; the reason is in the git history of those files"

edits = [
  {
    path = "dependencies[\"gnu.org/libidn2\"]"
    set = "*"
  },
]
