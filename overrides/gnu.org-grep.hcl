project = "gnu.org/grep"
why     = "converted from gnu.org-grep-bootstrap.patch; the reason is in the git history of those files"

edits = [
  {
    path = "build.dependencies[\"gnu.org/grep\"]"
    set = "*"
  },
]
